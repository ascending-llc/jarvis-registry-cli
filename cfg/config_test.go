package cfg

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadValid(t *testing.T) {
	cases := []struct {
		name            string
		wantBaseUrl     string
		wantAuthBaseUrl string
	}{
		{name: "valid-base-url", wantBaseUrl: "https://example.com", wantAuthBaseUrl: "https://example.com"},
		{name: "valid-http-localhost-url", wantBaseUrl: "http://localhost:8080", wantAuthBaseUrl: "http://localhost:8080"},
		{name: "valid-http-127-url", wantBaseUrl: "http://127.0.0.1:3000", wantAuthBaseUrl: "http://127.0.0.1:3000"},
		{name: "valid-explicit-auth-base-url", wantBaseUrl: "https://registry.example.com", wantAuthBaseUrl: "https://auth.example.com"},
		{name: "valid-local-auth-base-url", wantBaseUrl: "https://registry.example.com", wantAuthBaseUrl: "http://localhost:8888"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			config, err := Load(filepath.Join("testdata", c.name))
			require.NoError(t, err, "Load should succeed for a well-formed config")

			assert.Equal(t, c.wantBaseUrl, config.Registry.BaseUrl, "base_url should be trimmed of its trailing slash")
			assert.Equal(t, c.wantAuthBaseUrl, config.Registry.AuthBaseUrl, "auth_base_url should default to base_url when unset, or be preserved (trimmed) when set explicitly")
		})
	}
}

func TestLoadInvalid(t *testing.T) {
	cases := []struct {
		name        string
		wantErrText string
	}{
		{name: "invalid-http-scheme-url", wantErrText: "scheme must be https"},
		{name: "invalid-http-localhost-lookalike-host", wantErrText: "scheme must be https"},
		{name: "invalid-auth-base-url", wantErrText: "invalid registry.auth_base_url"},
		{name: "invalid-empty-url", wantErrText: "URL must not be empty"},
		{name: "invalid-no-host-url", wantErrText: "must include a host"},
		{name: "invalid-malformed-url", wantErrText: "not a valid URL"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Load(filepath.Join("testdata", c.name))
			require.Error(t, err, "Load should reject an invalid config")
			assert.Contains(t, err.Error(), c.wantErrText, "error message should explain why the config is invalid")
		})
	}
}
