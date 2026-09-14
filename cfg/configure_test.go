package cfg

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func TestConfigureCommandFirstRun(t *testing.T) {
	homeDir := t.TempDir()
	out, err := runConfigure(homeDir, "client.example.com\n1\n")
	require.NoError(t, err)

	configPath := filepath.Join(homeDir, RegistryDirName, "config.yaml")
	content, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.NotContains(t, string(content), "auth_base_url")

	config, err := Load(filepath.Dir(configPath))
	require.NoError(t, err)

	assert.Equal(t, "https://client.example.com", config.Registry.BaseUrl)
	assert.Equal(t, SkillsModeClaude, config.Local.Skills.Mode, "the first menu option (claude) should be persisted")
	assert.Contains(t, out, "✓ Configuration saved to "+configPath)
	assert.NotContains(t, out, "auth_base_url")
}

func TestConfigureCommandUpdatesOnlyBaseURL(t *testing.T) {
	homeDir := t.TempDir()
	registryDir := filepath.Join(homeDir, RegistryDirName)
	require.NoError(t, os.MkdirAll(registryDir, 0755))

	initial := `# keep this top-level comment
registry:
  base_url: https://old.example.com
  auth_base_url: http://localhost:8888 # keep this auth comment
  tenant: customer-one
feature:
  enabled: true
local:
  skills:
    mode: codex
`
	require.NoError(t, os.WriteFile(filepath.Join(registryDir, "config.yaml"), []byte(initial), 0600))

	// Update the base URL, then keep the existing skills mode with blank input.
	out, err := runConfigure(homeDir, "https://new.example.com\n\n")
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(registryDir, "config.yaml"))
	require.NoError(t, err)

	var want map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(initial), &want))
	wantRegistry, ok := want["registry"].(map[string]any)
	require.True(t, ok)

	wantRegistry["base_url"] = "https://new.example.com"

	var got map[string]any
	require.NoError(t, yaml.Unmarshal(content, &got))
	assert.Equal(t, want, got)
	assert.Contains(t, string(content), "# keep this top-level comment")
	assert.Contains(t, string(content), "# keep this auth comment")
	assert.NotContains(t, out, "auth_base_url")
}

func TestConfigureCommandKeepsCurrentValueOnEmptyInput(t *testing.T) {
	homeDir := t.TempDir()
	require.NoError(t, writeTestConfig(homeDir, "registry:\n  base_url: https://current.example.com\nlocal:\n  skills:\n    mode: copilot\n"))

	out, err := runConfigure(homeDir, "\n\n")
	require.NoError(t, err)

	config, err := Load(filepath.Join(homeDir, RegistryDirName))
	require.NoError(t, err)

	assert.Equal(t, "https://current.example.com", config.Registry.BaseUrl)
	assert.Equal(t, SkillsModeCopilot, config.Local.Skills.Mode, "blank input should keep the currently configured mode")
	assert.Contains(t, out, "✓ Configuration saved to ")
}

func TestConfigureCommandUpdatesExistingYMLFile(t *testing.T) {
	homeDir := t.TempDir()
	registryDir := filepath.Join(homeDir, RegistryDirName)
	require.NoError(t, os.MkdirAll(registryDir, 0755))

	ymlPath := filepath.Join(registryDir, "config.yml")
	require.NoError(t, os.WriteFile(ymlPath, []byte("registry:\n  base_url: https://old.example.com\n"), 0600))

	out, err := runConfigure(homeDir, "new.example.com\n1\n")
	require.NoError(t, err)

	config, err := Load(registryDir)
	require.NoError(t, err)
	assert.Equal(t, "https://new.example.com", config.Registry.BaseUrl)
	assert.Contains(t, out, "✓ Configuration saved to "+ymlPath)

	_, statErr := os.Stat(filepath.Join(registryDir, "config.yaml"))
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestConfigureCommandPreservesCommentOnlyConfig(t *testing.T) {
	homeDir := t.TempDir()
	initial := "# keep this first comment\n\n  # keep this indented comment\n"
	require.NoError(t, writeTestConfig(homeDir, initial))

	_, err := runConfigure(homeDir, "comments.example.com\n1\n")
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(homeDir, RegistryDirName, "config.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(content), "# keep this first comment")
	assert.Contains(t, string(content), "# keep this indented comment")

	config, err := Load(filepath.Join(homeDir, RegistryDirName))
	require.NoError(t, err)
	assert.Equal(t, "https://comments.example.com", config.Registry.BaseUrl)
}

func TestConfigureCommandRepromptsUntilInputIsValid(t *testing.T) {
	homeDir := t.TempDir()
	// base URL: blank (required), invalid scheme, then valid.
	// mode: out-of-range digit, non-numeric, then a valid choice (2 → codex).
	out, err := runConfigure(homeDir, "\nhttp://client.example.com\nvalid.example.com\n9\nabc\n2\n")
	require.NoError(t, err)

	config, err := Load(filepath.Join(homeDir, RegistryDirName))
	require.NoError(t, err)

	assert.Equal(t, "https://valid.example.com", config.Registry.BaseUrl)
	assert.Equal(t, SkillsModeCodex, config.Local.Skills.Mode)
	assert.Contains(t, out, "a value is required")
	assert.Contains(t, out, "scheme must be https")
	assert.Contains(t, out, "enter a number between 1 and 3")
}

func TestConfigureCommandPicksEachModeByDigit(t *testing.T) {
	cases := []struct {
		digit string
		want  SkillsMode
	}{
		{digit: "1", want: SkillsModeClaude},
		{digit: "2", want: SkillsModeCodex},
		{digit: "3", want: SkillsModeCopilot},
	}

	for _, c := range cases {
		t.Run(string(c.want), func(t *testing.T) {
			homeDir := t.TempDir()
			_, err := runConfigure(homeDir, "client.example.com\n"+c.digit+"\n")
			require.NoError(t, err)

			config, err := Load(filepath.Join(homeDir, RegistryDirName))
			require.NoError(t, err)
			assert.Equal(t, c.want, config.Local.Skills.Mode)
		})
	}
}

func TestPrintOptionsPrompt(t *testing.T) {
	cases := []struct {
		name    string
		current string
		want    string
	}{
		{name: "without current value", want: "Skills sync mode:\n  1) claude\n  2) codex\n  3) copilot\nChoose [1-3]: "},
		{name: "with current value", current: "codex", want: "Skills sync mode:\n  1) claude\n  2) codex\n  3) copilot\nChoose [1-3, current: codex]: "},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var out strings.Builder

			printOptionsPrompt(&out, "Skills sync mode", []string{"claude", "codex", "copilot"}, testCase.current)

			assert.Equal(t, testCase.want, out.String())
		})
	}
}

func TestResolveOption(t *testing.T) {
	options := []string{"claude", "codex", "copilot"}

	t.Run("resolves a valid 1-based index", func(t *testing.T) {
		got, err := resolveOption(options, "3")
		require.NoError(t, err)
		assert.Equal(t, "copilot", got)
	})

	for _, input := range []string{"0", "4", "abc", "-1", "1.5", ""} {
		t.Run("rejects "+input, func(t *testing.T) {
			_, err := resolveOption(options, input)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "enter a number between 1 and 3")
		})
	}
}

func TestPrintPrompt(t *testing.T) {
	cases := []struct {
		name    string
		current string
		want    string
	}{
		{name: "without current value", want: "Registry base URL: "},
		{name: "with current value", current: "https://current.example.com", want: "Registry base URL [https://current.example.com]: "},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var out strings.Builder

			printPrompt(&out, "Registry base URL", testCase.current)

			assert.Equal(t, testCase.want, out.String())
		})
	}
}

func TestNormalizeFieldValue(t *testing.T) {
	t.Run("uses field normalizer", func(t *testing.T) {
		field := configField{normalize: normalizeBaseURL}

		assert.Equal(t, "https://registry.example.com", normalizeFieldValue(field, "registry.example.com"))
		assert.Equal(t, "http://localhost:8080", normalizeFieldValue(field, "http://localhost:8080"))
	})

	t.Run("leaves fields without a normalizer unchanged", func(t *testing.T) {
		field := configField{}

		assert.Equal(t, "customer-one", normalizeFieldValue(field, "customer-one"))
	})
}

func TestConfigureCommandAcceptsLocalHTTP(t *testing.T) {
	cases := []string{
		"http://localhost:8080",
		"http://127.0.0.1:8080",
	}

	for _, value := range cases {
		t.Run(value, func(t *testing.T) {
			homeDir := t.TempDir()
			_, err := runConfigure(homeDir, value+"\n1\n")
			require.NoError(t, err)

			config, err := Load(filepath.Join(homeDir, RegistryDirName))
			require.NoError(t, err)
			assert.Equal(t, value, config.Registry.BaseUrl)
		})
	}
}

func TestConfigureCommandEOFDoesNotWrite(t *testing.T) {
	t.Run("missing config", func(t *testing.T) {
		homeDir := t.TempDir()
		_, err := runConfigure(homeDir, "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Registry base URL")

		_, statErr := os.Stat(filepath.Join(homeDir, RegistryDirName, "config.yaml"))
		assert.ErrorIs(t, statErr, os.ErrNotExist)
	})

	t.Run("existing config after invalid input", func(t *testing.T) {
		homeDir := t.TempDir()
		initial := []byte("registry:\n  base_url: https://current.example.com\n")
		require.NoError(t, writeTestConfig(homeDir, string(initial)))

		_, err := runConfigure(homeDir, "http://client.example.com\n")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Registry base URL")

		content, readErr := os.ReadFile(filepath.Join(homeDir, RegistryDirName, "config.yaml"))
		require.NoError(t, readErr)
		assert.Equal(t, initial, content)
	})
}

func runConfigure(homeDir string, input string) (string, error) {
	var out bytes.Buffer

	cmd := ConfigureCommand{
		logger:      log.New(&out, "", 0),
		stdin:       strings.NewReader(input),
		userHomeDir: homeDir,
	}

	if err := cmd.AfterApply(); err != nil {
		return out.String(), err
	}

	err := cmd.Run()

	return out.String(), err
}

func writeTestConfig(homeDir string, content string) error {
	registryDir := filepath.Join(homeDir, RegistryDirName)
	if err := os.MkdirAll(registryDir, 0755); err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(registryDir, "config.yaml"), []byte(content), 0600)
}
