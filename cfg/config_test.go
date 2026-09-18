package cfg

import (
	"os"
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
		wantSkipIds     []string
	}{
		{name: "valid-base-url", wantBaseUrl: "https://example.com", wantAuthBaseUrl: "https://example.com"},
		{name: "valid-http-localhost-url", wantBaseUrl: "http://localhost:8080", wantAuthBaseUrl: "http://localhost:8080"},
		{name: "valid-http-127-url", wantBaseUrl: "http://127.0.0.1:3000", wantAuthBaseUrl: "http://127.0.0.1:3000"},
		{name: "valid-explicit-auth-base-url", wantBaseUrl: "https://registry.example.com", wantAuthBaseUrl: "https://auth.example.com"},
		{name: "valid-local-auth-base-url", wantBaseUrl: "https://registry.example.com", wantAuthBaseUrl: "http://localhost:8888"},
		{name: "valid-skip-ids", wantBaseUrl: "https://example.com", wantAuthBaseUrl: "https://example.com", wantSkipIds: []string{"skill-id-1", "not-an-object-id"}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			config, err := Load(filepath.Join("testdata", c.name))
			require.NoError(t, err, "Load should succeed for a well-formed config")

			assert.Equal(t, c.wantBaseUrl, config.Registry.BaseUrl, "base_url should be trimmed of its trailing slash")
			assert.Equal(t, c.wantAuthBaseUrl, config.Registry.AuthBaseUrl, "auth_base_url should default to base_url when unset, or be preserved (trimmed) when set explicitly")
			assert.Equal(t, c.wantSkipIds, config.Local.Skills.SkipIds, "skip_ids should be trimmed and de-duplicated while preserving first-occurrence order, or nil when unset")
		})
	}
}

func TestLoadSkillsMode(t *testing.T) {
	t.Run("absent mode passes through as the unset zero value", func(t *testing.T) {
		config, err := Load(filepath.Join("testdata", "valid-base-url"))
		require.NoError(t, err, "a config with no local.skills.mode key must still load")
		assert.Equal(t, SkillsMode(""), config.Local.Skills.Mode, "an absent mode stays absent — no default is applied")
	})

	t.Run("a valid mode is accepted", func(t *testing.T) {
		config, err := Load(filepath.Join("testdata", "valid-skills-mode-codex"))
		require.NoError(t, err, "a config with a recognized mode must load")
		assert.Equal(t, SkillsModeCodex, config.Local.Skills.Mode, "the configured mode should be parsed through")
	})

	t.Run("an invalid mode is rejected with a path-qualified error", func(t *testing.T) {
		path := filepath.Join("testdata", "invalid-skills-mode")

		_, err := Load(path)
		require.Error(t, err, "an unrecognized mode must be rejected")
		assert.Contains(t, err.Error(), "invalid local.skills.mode in "+filepath.Join(path, "config.yaml"), "the error must name the offending file")
		assert.Contains(t, err.Error(), "claude, codex, copilot", "the error must name the valid modes")
	})
}

func TestLoadLinkOverride(t *testing.T) {
	for _, tc := range []struct {
		fixture string
		want    bool
	}{
		{fixture: "valid-base-url", want: false},
		{fixture: "valid-link-override", want: true},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			config, err := Load(filepath.Join("testdata", tc.fixture))
			require.NoError(t, err, "Load should succeed for a well-formed config")
			assert.Equal(t, tc.want, config.Local.Skills.Link.Override, "link.override should default false and parse true when set")
		})
	}
}

func TestLoadMissingConfig(t *testing.T) {
	registryDir := t.TempDir()

	_, err := Load(registryDir)
	require.Error(t, err)
	assert.Equal(t, "neither config.yaml nor config.yml exists in the "+registryDir+" folder", err.Error())
}

func TestLoadPreservesReadErrorBehavior(t *testing.T) {
	registryDir := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(registryDir, []byte("not a directory"), 0600))

	_, err := Load(registryDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read config file at "+filepath.Join(registryDir, "config.yaml"))
	assert.NotContains(t, err.Error(), "failed to resolve config file")
}

func TestLoadInvalid(t *testing.T) {
	cases := []struct {
		name            string
		wantErrText     string
		wantErrContains []string
	}{
		{name: "invalid-http-scheme-url", wantErrText: "scheme must be https"},
		{name: "invalid-http-localhost-lookalike-host", wantErrText: "scheme must be https"},
		{name: "invalid-auth-base-url", wantErrText: "invalid registry.auth_base_url"},
		{name: "invalid-empty-url", wantErrText: "URL must not be empty"},
		{name: "invalid-no-host-url", wantErrText: "must include a host"},
		{name: "invalid-malformed-url", wantErrText: "not a valid URL"},
		{name: "invalid-empty-skip-id-entry", wantErrText: "invalid local.skills.skip_ids", wantErrContains: []string{filepath.Join("testdata", "invalid-empty-skip-id-entry", "config.yaml"), "entry 1 is empty"}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Load(filepath.Join("testdata", c.name))
			require.Error(t, err, "Load should reject an invalid config")
			assert.Contains(t, err.Error(), c.wantErrText, "error message should explain why the config is invalid")

			for _, want := range c.wantErrContains {
				assert.Contains(t, err.Error(), want, "error message should also contain %q", want)
			}
		})
	}
}
