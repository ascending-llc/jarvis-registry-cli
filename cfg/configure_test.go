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
	out, err := runConfigure(homeDir, "client.example.com\n")
	require.NoError(t, err)

	configPath := filepath.Join(homeDir, RegistryDirName, "config.yaml")
	content, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.NotContains(t, string(content), "auth_base_url")

	config, err := Load(filepath.Dir(configPath))
	require.NoError(t, err)

	assert.Equal(t, "https://client.example.com", config.Registry.BaseUrl)
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
`
	require.NoError(t, os.WriteFile(filepath.Join(registryDir, "config.yaml"), []byte(initial), 0600))

	out, err := runConfigure(homeDir, "https://new.example.com\n")
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
	require.NoError(t, writeTestConfig(homeDir, "registry:\n  base_url: https://current.example.com\n"))

	out, err := runConfigure(homeDir, "\n")
	require.NoError(t, err)

	config, err := Load(filepath.Join(homeDir, RegistryDirName))
	require.NoError(t, err)

	assert.Equal(t, "https://current.example.com", config.Registry.BaseUrl)
	assert.Contains(t, out, "✓ Configuration saved to ")
}

func TestConfigureCommandUpdatesExistingYMLFile(t *testing.T) {
	homeDir := t.TempDir()
	registryDir := filepath.Join(homeDir, RegistryDirName)
	require.NoError(t, os.MkdirAll(registryDir, 0755))

	ymlPath := filepath.Join(registryDir, "config.yml")
	require.NoError(t, os.WriteFile(ymlPath, []byte("registry:\n  base_url: https://old.example.com\n"), 0600))

	out, err := runConfigure(homeDir, "new.example.com\n")
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

	_, err := runConfigure(homeDir, "comments.example.com\n")
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
	out, err := runConfigure(homeDir, "\nhttp://client.example.com\nvalid.example.com\n")
	require.NoError(t, err)

	config, err := Load(filepath.Join(homeDir, RegistryDirName))
	require.NoError(t, err)

	assert.Equal(t, "https://valid.example.com", config.Registry.BaseUrl)
	assert.Contains(t, out, "a value is required")
	assert.Contains(t, out, "scheme must be https")
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
			_, err := runConfigure(homeDir, value+"\n")
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
