package skills

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// ensurePluginRootConsent gates any filesystem mutation of a pre-existing
// c.pluginRoot that this CLI did not create: a brand-new plugin root
// needs no confirmation, one already carrying this CLI's skill-lock.json
// marker is trusted silently, and anything else either prompts an
// interactive user (via c.isTerminal/c.stdin, swappable in tests) for
// confirmation or fails loudly when stdin isn't a terminal. This must run
// before any filesystem mutation, including creating c.pluginRoot itself.
func (c *SyncCommand) ensurePluginRootConsent() error {
	if _, err := os.Stat(c.pluginRoot); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("plugin folder %s exists but cannot be queried for stat: %s", c.pluginRoot, err.Error())
	}

	manifest, err := c.mrw.ReadManifest()
	if err != nil {
		return err
	}

	if manifest.ManagedBy == managedByValue {
		return nil
	}

	if !c.isTerminal() {
		return fmt.Errorf("%s exists but was not created by jarvis-registry-cli (no valid skill-lock.json marker found). Refusing to modify it non-interactively. Run `jarvis-registry sync-skills` from an interactive terminal to confirm, or remove the folder manually", c.pluginRoot)
	}

	fmt.Fprintf(os.Stderr, "%s exists but was not created by jarvis-registry-cli. Proceed and manage it? [y/N] ", c.pluginRoot)

	var response string

	_, _ = fmt.Fscanln(c.stdin, &response)

	if !strings.EqualFold(response, "y") && !strings.EqualFold(response, "yes") {
		return fmt.Errorf("aborted: %s was not confirmed as safe to manage", c.pluginRoot)
	}

	return nil
}
