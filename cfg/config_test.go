package cfg

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadValid(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err, "should be able to locate the user home directory")

	wantDest := filepath.Join(home, "jarvis-registry-cli-test-dest")

	cases := []struct {
		name        string
		wantBaseUrl string
	}{
		{name: "valid-tilde-dest", wantBaseUrl: "https://example.com"},
		{name: "valid-dollar-home-dest", wantBaseUrl: "https://example.com"},
		{name: "valid-braced-home-dest", wantBaseUrl: "HTTPS://example.com/api"},
		{name: "valid-percent-userprofile-dest", wantBaseUrl: "https://example.com"},
		{name: "valid-dollar-userprofile-dest", wantBaseUrl: "https://example.com"},
		{name: "valid-http-localhost-url", wantBaseUrl: "http://localhost:8080"},
		{name: "valid-http-127-url", wantBaseUrl: "http://127.0.0.1:3000"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			config, err := Load(filepath.Join("testdata", c.name))
			require.NoError(t, err, "Load should succeed for a well-formed config")

			assert.Equal(t, wantDest, config.Local.Dest, "destination_folder should resolve to the expanded home-relative path")
			assert.Equal(t, c.wantBaseUrl, config.Registry.BaseUrl, "base_url should be trimmed of its trailing slash")
		})
	}
}

func TestLoadInvalid(t *testing.T) {
	cases := []struct {
		name        string
		wantErrText string
	}{
		{name: "invalid-empty-dest", wantErrText: "must not be empty"},
		{name: "invalid-relative-dest", wantErrText: "must be absolute"},
		{name: "invalid-other-user-tilde-dest", wantErrText: `"~user"`},
		{name: "invalid-home-dest", wantErrText: "unsafe"},
		{name: "invalid-http-scheme-url", wantErrText: "scheme must be https"},
		{name: "invalid-http-localhost-lookalike-host", wantErrText: "scheme must be https"},
		{name: "invalid-empty-url", wantErrText: "URL must not be empty"},
		{name: "invalid-no-host-url", wantErrText: "must include a host"},
		{name: "invalid-malformed-url", wantErrText: "not a valid URL"},
	}

	if runtime.GOOS != "windows" {
		cases = append(cases, struct {
			name        string
			wantErrText string
		}{name: "invalid-root-dest", wantErrText: "unsafe"})
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Load(filepath.Join("testdata", c.name))
			require.Error(t, err, "Load should reject an invalid config")
			assert.Contains(t, err.Error(), c.wantErrText, "error message should explain why the config is invalid")
		})
	}
}
