package common

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
)

func TestLogger_InvalidLogLevel(t *testing.T) {
	_, err := Logger("invalid")
	require.EqualError(t, err, "unknown log level: invalid")
}

func TestLogger(t *testing.T) {
	lgr, err := Logger("debug")
	require.NoError(t, err)
	require.NotNil(t, lgr)
	require.True(t, lgr.IsDebug())
}

func TestValidatePort(t *testing.T) {
	err := ValidatePort("-test-flag-name", "1234")
	require.NoError(t, err)
	err = ValidatePort("-test-flag-name", "invalid-port")
	require.EqualError(t, err, "-test-flag-name value of invalid-port is not a valid integer.")
	err = ValidatePort("-test-flag-name", "22")
	require.EqualError(t, err, "-test-flag-name value of 22 is not in the port range 1024-65535.")
}

// TestConsulAclLogin ensures that our implementation of consul login hits `/v1/acl/login`.
func TestConsulAclLogin(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	type APICall struct {
		Method string
		Path   string
	}
	var consulAPICalls []APICall

	// Stub the read of the bearerTokenFile inside the login function.
	bearerTokenFile, err := ioutil.TempFile("", "bearerTokenFile")
	require.NoError(err)
	_, err = bearerTokenFile.WriteString("foo")
	require.NoError(err)

	// Start the Consul server.
	consulServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Record all the API calls made.
		consulAPICalls = append(consulAPICalls, APICall{
			Method: r.Method,
			Path:   r.URL.Path,
		})
	}))
	defer consulServer.Close()
	serverURL, err := url.Parse(consulServer.URL)
	require.NoError(err)
	clientConfig := &api.Config{Address: serverURL.String()}
	client, err := api.NewClient(clientConfig)
	require.NoError(err)

	// We expect this to fail in the internal client.ACL().Login() path because we've not
	// bootstrapped any auth methods.
	err = ConsulAclLogin(
		client,
		bearerTokenFile.Name(),
		"foo",
		"/foo",
		"/foo",
		nil,
	)
	require.Error(err, "error logging in: EOF")
	// Ensure that the /v1/acl/login url was correctly hit.
	require.Equal([]APICall{
		{
			"POST",
			"/v1/acl/login",
		},
	}, consulAPICalls)
}
