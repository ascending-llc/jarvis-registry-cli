package cfg

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveConfigPath(t *testing.T) {
	registryDir := t.TempDir()

	path, existed, err := resolveConfigPath(registryDir)
	require.NoError(t, err)
	assert.False(t, existed)
	assert.Equal(t, filepath.Join(registryDir, "config.yaml"), path)

	ymlPath := filepath.Join(registryDir, "config.yml")
	require.NoError(t, os.WriteFile(ymlPath, []byte("{}\n"), 0600))

	path, existed, err = resolveConfigPath(registryDir)
	require.NoError(t, err)
	assert.True(t, existed)
	assert.Equal(t, ymlPath, path)

	yamlPath := filepath.Join(registryDir, "config.yaml")
	require.NoError(t, os.WriteFile(yamlPath, []byte("{}\n"), 0600))

	path, existed, err = resolveConfigPath(registryDir)
	require.NoError(t, err)
	assert.True(t, existed)
	assert.Equal(t, yamlPath, path)
}

func TestConfigReadWriterGetSetWrite(t *testing.T) {
	registryDir := t.TempDir()
	configPath := filepath.Join(registryDir, "config.yaml")
	initial := `# configuration comment
registry:
  base_url: https://old.example.com
  auth_base_url: http://localhost:8888 # auth comment
other:
  value: untouched
`
	require.NoError(t, os.WriteFile(configPath, []byte(initial), 0600))

	rw, err := NewConfigReadWriter(registryDir)
	require.NoError(t, err)
	assert.Equal(t, "https://old.example.com", rw.Get([]string{"registry", "base_url"}))
	assert.Equal(t, "http://localhost:8888", rw.Get([]string{"registry", "auth_base_url"}))
	assert.Empty(t, rw.Get([]string{"registry", "missing"}))

	rw.Set([]string{"registry", "base_url"}, "https://new.example.com")
	require.NoError(t, rw.Write())

	written, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(written), "# configuration comment")
	assert.Contains(t, string(written), "# auth comment")

	reread, err := NewConfigReadWriter(registryDir)
	require.NoError(t, err)
	assert.Equal(t, "https://new.example.com", reread.Get([]string{"registry", "base_url"}))
	assert.Equal(t, "http://localhost:8888", reread.Get([]string{"registry", "auth_base_url"}))
	assert.Equal(t, "untouched", reread.Get([]string{"other", "value"}))

	matches, err := filepath.Glob(filepath.Join(registryDir, ".config-*.yaml.tmp"))
	require.NoError(t, err)
	assert.Empty(t, matches)
}

func TestConfigReadWriterCreatesNestedConfig(t *testing.T) {
	registryDir := filepath.Join(t.TempDir(), RegistryDirName)
	rw, err := NewConfigReadWriter(registryDir)
	require.NoError(t, err)

	rw.Set([]string{"registry", "base_url"}, "https://new.example.com")
	require.NoError(t, rw.Write())

	reread, err := NewConfigReadWriter(registryDir)
	require.NoError(t, err)
	assert.Equal(t, "https://new.example.com", reread.Get([]string{"registry", "base_url"}))
}

func TestNewConfigReadWriterRejectsMalformedYAML(t *testing.T) {
	registryDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(registryDir, "config.yaml"), []byte("registry: [\n"), 0600))

	_, err := NewConfigReadWriter(registryDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse config file")
}
