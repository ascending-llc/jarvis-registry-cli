package cfg

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadValid(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err, "should be able to locate the user home directory")

	testDestPluginRoot := filepath.Join(home, "jarvis-registry-cli-test-dest", ".claude", "skills", "jarvis-registry")
	testDestDest := filepath.Join(testDestPluginRoot, "skills")

	homePluginRoot := filepath.Join(home, ".claude", "skills", "jarvis-registry")
	homeDest := filepath.Join(homePluginRoot, "skills")

	customPluginRoot := filepath.Join(home, "some-project", ".claude", "skills", "jarvis-registry")
	customDest := filepath.Join(customPluginRoot, "skills")

	cases := []struct {
		name            string
		wantBaseUrl     string
		wantAuthBaseUrl string
		wantPluginRoot  string
		wantDest        string
	}{
		{name: "valid-tilde-project-path", wantBaseUrl: "https://example.com", wantAuthBaseUrl: "https://example.com", wantPluginRoot: testDestPluginRoot, wantDest: testDestDest},
		{name: "valid-dollar-home-project-path", wantBaseUrl: "https://example.com", wantAuthBaseUrl: "https://example.com", wantPluginRoot: testDestPluginRoot, wantDest: testDestDest},
		{name: "valid-braced-home-project-path", wantBaseUrl: "HTTPS://example.com/api", wantAuthBaseUrl: "HTTPS://example.com/api", wantPluginRoot: testDestPluginRoot, wantDest: testDestDest},
		{name: "valid-percent-userprofile-project-path", wantBaseUrl: "https://example.com", wantAuthBaseUrl: "https://example.com", wantPluginRoot: testDestPluginRoot, wantDest: testDestDest},
		{name: "valid-dollar-userprofile-project-path", wantBaseUrl: "https://example.com", wantAuthBaseUrl: "https://example.com", wantPluginRoot: testDestPluginRoot, wantDest: testDestDest},
		{name: "valid-http-localhost-url", wantBaseUrl: "http://localhost:8080", wantAuthBaseUrl: "http://localhost:8080", wantPluginRoot: testDestPluginRoot, wantDest: testDestDest},
		{name: "valid-http-127-url", wantBaseUrl: "http://127.0.0.1:3000", wantAuthBaseUrl: "http://127.0.0.1:3000", wantPluginRoot: testDestPluginRoot, wantDest: testDestDest},
		{name: "valid-explicit-auth-base-url", wantBaseUrl: "https://registry.example.com", wantAuthBaseUrl: "https://auth.example.com", wantPluginRoot: testDestPluginRoot, wantDest: testDestDest},
		{name: "valid-local-auth-base-url", wantBaseUrl: "https://registry.example.com", wantAuthBaseUrl: "http://localhost:8888", wantPluginRoot: testDestPluginRoot, wantDest: testDestDest},
		{name: "valid-empty-project-path", wantBaseUrl: "https://example.com", wantAuthBaseUrl: "https://example.com", wantPluginRoot: homePluginRoot, wantDest: homeDest},
		{name: "valid-omitted-project-path", wantBaseUrl: "https://example.com", wantAuthBaseUrl: "https://example.com", wantPluginRoot: homePluginRoot, wantDest: homeDest},
		{name: "valid-custom-project-path", wantBaseUrl: "https://example.com", wantAuthBaseUrl: "https://example.com", wantPluginRoot: customPluginRoot, wantDest: customDest},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			config, err := Load(filepath.Join("testdata", c.name))
			require.NoError(t, err, "Load should succeed for a well-formed config")

			assert.Equal(t, c.wantPluginRoot, config.Local.PluginRoot, "project_path should resolve to the expected plugin root")
			assert.Equal(t, c.wantDest, config.Local.Dest, "project_path should resolve to the expected skills destination")
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
		{name: "invalid-relative-project-path", wantErrText: "must be absolute"},
		{name: "invalid-other-user-tilde-project-path", wantErrText: `"~user"`},
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
