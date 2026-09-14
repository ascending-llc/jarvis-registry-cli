package skills

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

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
// values, substituting an explicit placeholder for the unset/empty case.
func (c *ShowCommand) Run() error {
	mode := string(c.mode)
	if mode == "" {
		mode = "(not set)"
	}

	c.logger.Printf("Skill sync mode: %s\n", mode)

	skipIds := "(none)"
	if len(c.skipIds) > 0 {
		skipIds = strings.Join(c.skipIds, ", ")
	}

	c.logger.Printf("Skip IDs: %s\n", skipIds)

	return nil
}
