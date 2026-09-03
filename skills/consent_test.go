package skills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyncCommandEnsurePluginRootConsent(t *testing.T) {
	t.Run("a brand-new plugin root needs no confirmation", func(t *testing.T) {
		pluginRoot := filepath.Join(t.TempDir(), "does-not-exist-yet")

		c := &SyncCommand{
			pluginRoot: pluginRoot,
			mrw:        NewManifestReadWriter(pluginRoot),
			isTerminal: func() bool {
				t.Fatal("isTerminal should not be consulted for a brand-new plugin root")

				return false
			},
			stdin: strings.NewReader(""),
		}

		err := c.ensurePluginRootConsent()
		assert.NoError(t, err, "a plugin root that does not exist yet should need no confirmation")
	})

	t.Run("a plugin root already carrying this CLI's marker proceeds silently", func(t *testing.T) {
		pluginRoot := t.TempDir()

		writeConsentTestManifest(t, pluginRoot, managedByValue)

		c := &SyncCommand{
			pluginRoot: pluginRoot,
			mrw:        NewManifestReadWriter(pluginRoot),
			isTerminal: func() bool {
				t.Fatal("isTerminal should not be consulted once a trusted marker is found")

				return false
			},
			stdin: strings.NewReader(""),
		}

		err := c.ensurePluginRootConsent()
		assert.NoError(t, err, "a plugin root already marked managedBy this CLI should proceed without confirmation")
	})

	t.Run("a foreign plugin root fails loudly when stdin is not a terminal", func(t *testing.T) {
		pluginRoot := t.TempDir()

		require.NoError(t, os.WriteFile(filepath.Join(pluginRoot, "some-other-file"), []byte("not ours"), 0644), "should be able to write a foreign file into the plugin root")

		c := &SyncCommand{
			pluginRoot: pluginRoot,
			mrw:        NewManifestReadWriter(pluginRoot),
			isTerminal: func() bool { return false },
			stdin:      strings.NewReader(""),
		}

		err := c.ensurePluginRootConsent()
		require.Error(t, err, "a foreign plugin root should be refused when stdin is not a terminal")
		assert.Contains(t, err.Error(), "Refusing to modify it non-interactively", "the error should explain why it refused")
	})

	t.Run("a foreign plugin root proceeds when an interactive user confirms", func(t *testing.T) {
		for _, response := range []string{"y", "Y", "yes", "YES"} {
			t.Run(response, func(t *testing.T) {
				pluginRoot := t.TempDir()

				require.NoError(t, os.WriteFile(filepath.Join(pluginRoot, "some-other-file"), []byte("not ours"), 0644), "should be able to write a foreign file into the plugin root")

				c := &SyncCommand{
					pluginRoot: pluginRoot,
					mrw:        NewManifestReadWriter(pluginRoot),
					isTerminal: func() bool { return true },
					stdin:      strings.NewReader(response + "\n"),
				}

				err := c.ensurePluginRootConsent()
				assert.NoError(t, err, "an interactive user answering %q should be treated as confirming", response)
			})
		}
	})

	t.Run("a foreign plugin root aborts when an interactive user declines or gives no answer", func(t *testing.T) {
		for _, response := range []string{"n", "no", "", "garbage"} {
			t.Run(response, func(t *testing.T) {
				pluginRoot := t.TempDir()

				require.NoError(t, os.WriteFile(filepath.Join(pluginRoot, "some-other-file"), []byte("not ours"), 0644), "should be able to write a foreign file into the plugin root")

				c := &SyncCommand{
					pluginRoot: pluginRoot,
					mrw:        NewManifestReadWriter(pluginRoot),
					isTerminal: func() bool { return true },
					stdin:      strings.NewReader(response + "\n"),
				}

				err := c.ensurePluginRootConsent()
				require.Error(t, err, "an interactive user answering %q should abort", response)
				assert.Contains(t, err.Error(), "was not confirmed as safe to manage", "the error should explain the abort")
			})
		}
	})

	t.Run("a manifest marked managedBy a different value is treated as foreign", func(t *testing.T) {
		pluginRoot := t.TempDir()

		writeConsentTestManifest(t, pluginRoot, "some-other-tool")

		c := &SyncCommand{
			pluginRoot: pluginRoot,
			mrw:        NewManifestReadWriter(pluginRoot),
			isTerminal: func() bool { return false },
			stdin:      strings.NewReader(""),
		}

		err := c.ensurePluginRootConsent()
		require.Error(t, err, "a manifest managed by a different tool should be treated as foreign")
		assert.Contains(t, err.Error(), "Refusing to modify it non-interactively", "the error should explain why it refused")
	})
}

// writeConsentTestManifest writes a minimal skill-lock.json to pluginRoot
// (creating it first) with the given managedBy value.
func writeConsentTestManifest(t *testing.T, pluginRoot, managedBy string) {
	t.Helper()

	manifest := ManifestV1{SchemaVersion: manifestSchemaVersion, Description: manifestDescription, ManagedBy: managedBy}

	body, err := json.Marshal(manifest)
	require.NoError(t, err, "should be able to marshal the test manifest")

	require.NoError(t, os.WriteFile(filepath.Join(pluginRoot, manifestFileName), body, 0644), "should be able to write the test manifest")
}
