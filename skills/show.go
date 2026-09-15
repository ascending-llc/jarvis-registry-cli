package skills

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/ascending-llc/jarvis-registry-cli/cfg"
)

// ShowCommand implements the "skills show" subcommand: it prints the
// resolved local.skills settings from config, without touching the
// Registry or the filesystem beyond the config file itself.
type ShowCommand struct {
	logger         Logger
	configLoadFunc func(string) (cfg.Config, error)
	userHomeDir    string
	registryDir    string
	mode           cfg.SkillsMode
	skipIds        []string
}

// BeforeReset sets defaults for ShowCommand that don't depend on parsed
// flags: the user's home directory, the config loader, and the logger.
func (c *ShowCommand) BeforeReset() (err error) {
	if c.userHomeDir, err = os.UserHomeDir(); err != nil {
		return fmt.Errorf("could not locate user home directory: %s", err.Error())
	}

	c.configLoadFunc = cfg.Load

	c.logger = log.New(os.Stdout, "", 0)

	return nil
}

// AfterApply derives ShowCommand's remaining dependencies from the loaded
// config: the registry directory, and the resolved local.skills.mode and
// local.skills.skip_ids values.
func (c *ShowCommand) AfterApply() (err error) {
	c.registryDir = filepath.Join(c.userHomeDir, cfg.RegistryDirName)

	config, err := c.configLoadFunc(c.registryDir)
	if err != nil {
		return fmt.Errorf("failed to load config options: %s", err.Error())
	}

	c.mode = config.Local.Skills.Mode
	c.skipIds = config.Local.Skills.SkipIds

	return nil
}

// Run prints the resolved local.skills.mode and local.skills.skip_ids
// values, substituting an explicit placeholder for the unset mode. The
// skip_ids line is omitted entirely when empty, since skip_ids is an
// opt-in setting not advertised by `jarvis-registry configure`; printing
// it as unset could otherwise surprise users who never configured it.
// skip_ids, when non-empty, is printed as a Markdown-style unordered list
// so a long list of Ids doesn't run together on one line.
func (c *ShowCommand) Run() error {
	mode := string(c.mode)
	if mode == "" {
		mode = "(not set)"
	}

	c.logger.Printf("Skill sync mode: %s\n", mode)

	if len(c.skipIds) == 0 {
		return nil
	}

	c.logger.Println("Skip IDs:")

	for _, id := range c.skipIds {
		c.logger.Printf("  - %s\n", id)
	}

	return nil
}
