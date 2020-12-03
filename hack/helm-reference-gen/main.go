package main

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"
)

var typeFromDescription = regexp.MustCompile(`(?m).*type: (.*)$`)
var defaultFromDescription = regexp.MustCompile(`(?m).*default: (.*)$`)
var commentPrefix = regexp.MustCompile(`[^\S\n]*#[^\S\n]?`)

type YAMLNode struct {
	Indent       int
	ParentAnchor string
	ParentWasMap bool
	Key          string
	Default      string
	Description  string
	KindTag      string
	Children     []YAMLNode
}

func (y YAMLNode) Anchor() string {
	return fmt.Sprintf("%s-%s", y.ParentAnchor, strings.ToLower(y.Key))
}

func (y YAMLNode) FormattedDefault() string {
	 if match := defaultFromDescription.FindStringSubmatch(y.Description); len(match) > 0 {
		return match[1]
	}
	if y.Kind() == "array<map>" {
		return ""
	}
	if y.Default != "" {
		// Don't show multiline string defaults since it wouldn't fit.
		// We use > 2 because if it's extraConfig, e.g. `{}` then we want to
		// show it but if it's affinity then it doesn't make sense to show it.
		if len(strings.Split(y.Default, "\n")) > 2 {
			return ""
		}
		return strings.TrimSpace(y.Default)
	}
	return `""`
}

func (y YAMLNode) FormattedDescription() string {
	withoutCommentPrefix := commentPrefix.ReplaceAllString(y.Description, "")
	var withWhitespaceIndent string
	lines := strings.Split(withoutCommentPrefix, "\n")
	for i, line := range lines {

		// If the line is a type: or default: override we don't include it in
		// the markdown description.
		// This check must be before the i == 0 check because if there's only
		// one line in the description and it's the type description then we
		// want to discard it.
		if strings.HasPrefix(line, "type:") || strings.HasPrefix(line, "default:") {
				continue
		}
		if i == 0 {
			// The first line is printed inline with the key information so it
			// doesn't need to be indented.
			withWhitespaceIndent = line + "\n"
			continue
		}
		if line != "" {
			indent := y.Indent + 1
			if y.ParentWasMap {
				indent = y.Indent
			}
			withWhitespaceIndent += fmt.Sprintf("%s%s\n", strings.Repeat(" ", indent), line)
		} else {
			// No need to add whitespace indent to a newline.
			withWhitespaceIndent += "\n"
		}
	}
	// Trim final newline.
	return strings.TrimSuffix(withWhitespaceIndent, "\n")
}

func (y YAMLNode) Kind() string {
	if match := typeFromDescription.FindStringSubmatch(y.Description); len(match) > 0 {
		return match[1]
	}

	switch strings.TrimLeft(y.KindTag, "!") {
	case "str":
		return "string"
	case "int":
		return "integer"
	case "bool":
		return "boolean"
	case "map":
		return "map"
	default:
		return fmt.Sprintf("unknown kind '%v'", y.KindTag)
	}
}

func (y YAMLNode) LeadingIndent() string {
	indent := y.Indent - 1
	if y.ParentWasMap {
		indent = y.Indent - 3
	}
	return strings.Repeat(" ", indent)
}

var tmpl = `{{ .LeadingIndent }}- ${{ .Key }}$ ((#v{{ .Anchor }})){{ if ne .Kind "map" }} (${{ .Kind }}{{ if .FormattedDefault }}: {{ .FormattedDefault }}{{ end }}$){{ end }}{{ if .FormattedDescription}} - {{ .FormattedDescription }}{{ end }}`

func main() {

}

func Parse(yamlStr string) (YAMLNode, error) {
	var node yaml.Node
	err := yaml.Unmarshal([]byte(yamlStr), &node)
	if err != nil {
		return YAMLNode{}, err
	}
	return ParseNode(&node, ""), nil
}

func ParseChildren(n *yaml.Node, parentAnchor string, parentWasMap bool) []YAMLNode {
	var children []YAMLNode
	skipNext := false
	for i, child := range n.Content {
		if skipNext {
			skipNext = false
			continue
		}

		if child.Kind == yaml.ScalarNode {
			if len(n.Content) < i+1 {
				panic("wrong length")
			}
			next := n.Content[i+1]

			switch next.Kind {
			case yaml.ScalarNode:
				children = append(children, YAMLNode{
					ParentAnchor: parentAnchor,
					ParentWasMap: parentWasMap,
					Indent:       child.Column,
					Key:          child.Value,
					Description:  child.HeadComment,
					KindTag:      next.Tag,
					Default:      next.Value,
				})

			case yaml.MappingNode:
				mapNode := YAMLNode{
					ParentAnchor: parentAnchor,
					ParentWasMap: parentWasMap,
					Indent:       child.Column,
					Key:          child.Value,
					Description:  child.HeadComment,
					KindTag:      next.Tag,
				}
				mapNode.Children = ParseChildren(next, mapNode.Anchor(), false)
				children = append(children, mapNode)

			case yaml.SequenceNode:
				if len(next.Content) == 0 {
					seqNode := YAMLNode{
						ParentAnchor: parentAnchor,
						ParentWasMap: parentWasMap,
						Indent:       child.Column,
						Key:          child.Value,
						// Default is empty array.
						Default:      "[]",
						Description:  child.HeadComment,
						KindTag:      next.Tag,
					}
					children = append(children, seqNode)
				} else if allScalars(next.Content) {
					seqNode := YAMLNode{
						ParentAnchor: parentAnchor,
						ParentWasMap: parentWasMap,
						Indent:       child.Column,
						Key:          child.Value,
						// Default will be the yaml value.
						Default:      toYaml(next.Content),
						Description:  child.HeadComment,
						KindTag:      next.Tag,
					}
					children = append(children, seqNode)
				} else {
					seqNode := YAMLNode{
						ParentAnchor: parentAnchor,
						ParentWasMap: parentWasMap,
						Indent:       child.Column,
						Key:          child.Value,
						Description:  child.HeadComment,
						KindTag:      next.Tag,
					}
					seqNode.Children = ParseChildren(next, seqNode.Anchor(), false)
					children = append(children, seqNode)
				}
			}

			skipNext = true
			continue
		}

		if len(n.Content) == 1 {
			// We basically skip this node.
			return ParseChildren(n.Content[0], parentAnchor, true)
		}

		children = append(children, ParseNode(child, parentAnchor))
	}
	return children
}

func allScalars(content []*yaml.Node) bool {
	for _, n := range content {
		if n.Kind != yaml.ScalarNode || len(n.Content) > 0 {
			return false
		}
	}
	return true
}

func toYaml(content []*yaml.Node) string {
	type Intermediary struct {
		Arr []*yaml.Node `yaml:"arr,flow"`
	}
	i := Intermediary{
		Arr: content,
	}
	out, err := yaml.Marshal(i)
	if err != nil {
		panic(err)
	}
	// todo: reduce hackiness.
	return strings.TrimPrefix(string(out), "arr: ")
}

func ParseNode(n *yaml.Node, parentAnchor string) YAMLNode {
	var thisNode YAMLNode
	switch n.Kind {
	case yaml.DocumentNode:
		// todo: handle content len == 0.
		return ParseNode(n.Content[0], "")
	case yaml.MappingNode:
		if parentAnchor == "" {
			// This is the root map.
			return YAMLNode{
				Indent:   0,
				Children: ParseChildren(n, "", false),
			}
		}
		return YAMLNode{
			Indent:       n.Column,
			ParentAnchor: parentAnchor,
			Key:          n.Value,
			Default:      "",
			Description:  n.HeadComment,
			KindTag:      n.Tag,
			Children: ParseChildren(n, parentAnchor, false),
		}
	case yaml.ScalarNode:
		panic("scalars should not be parsed here")

	}
	return thisNode
}

func Gen(yamlStr string) (string, error) {
	node, err := Parse(yamlStr)
	if err != nil {
		return "", err
	}

	tm := template.Must(template.New("").Parse(strings.Replace(tmpl, "$", "`", -1)))
	children, err := GenChildren(tm, node)
	return strings.Join(children, "\n\n"), err
}

func GenChildren(tm *template.Template, node YAMLNode) ([]string, error) {
	var out []string
	for _, child := range node.Children {
		var nodeOut bytes.Buffer
		err := tm.Execute(&nodeOut, child)
		if err != nil {
			return nil, err
		}
		childOut, err := GenChildren(tm, child)
		if err != nil {
			return nil, err
		}
		out = append(append(out, nodeOut.String()), childOut...)
	}
	return out, nil
}


// todo: enterprise Alert <EnterpriseAlert inline /> -
// todo: links to other settings
