package skills

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/ascending-llc/jarvis-registry-cli/cfg"
)

// ensureSyncRootConsent gates any filesystem mutation of a pre-existing
// c.syncRoot that this CLI did not create: a brand-new sync root needs no
// confirmation, one already carrying this CLI's skill-lock.json marker is
// trusted silently, and anything else either prompts an interactive user
// (via c.isTerminal/c.stdin, swappable in tests) for confirmation or fails
// loudly when stdin isn't a terminal. For project-scope codex/copilot the
// prompt carries an extra warning, since that sync root is an
// ecosystem-shared directory other tools (e.g. `gh skill install`) also
// write into, so the exposure of managing it is materially higher than for
// claude's plugin-owned subtree or a personal-scope CLI-owned root. This
// must run before any filesystem mutation, including creating c.syncRoot
// itself.
func (c *SyncCommand) ensureSyncRootConsent() error {
	if _, err := os.Stat(c.syncRoot); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("folder %s exists but cannot be queried for stat: %s", c.syncRoot, err.Error())
	}

	manifest, err := c.mrw.ReadManifest()
	if err != nil {
		return err
	}

	if manifest.ManagedBy == managedByValue {
		return nil
	}

	warning := ""
	if c.mode != cfg.SkillsModeClaude && !c.personalScope {
		warning = " Note: this folder may also be managed directly by other tools (e.g. `gh skill install`) or contain skills you wrote by hand — anything not tracked by this CLI's own sync will be deleted on future syncs."
	}

	if !c.isTerminal() {
		return fmt.Errorf("%s exists but was not created by jarvis-registry-cli (no valid skill-lock.json marker found).%s Refusing to modify it non-interactively. Re-run the same skills sync command (with the same project path and --mode) from an interactive terminal to confirm, or remove the folder manually", c.syncRoot, warning)
	}

	fmt.Fprintf(os.Stderr, "%s exists but was not created by jarvis-registry-cli.%s Proceed and manage it? [y/N] ", c.syncRoot, warning)

	var response string

	_, _ = fmt.Fscanln(c.stdin, &response)

	if !strings.EqualFold(response, "y") && !strings.EqualFold(response, "yes") {
		return fmt.Errorf("aborted: %s was not confirmed as safe to manage", c.syncRoot)
	}

	return nil
}
