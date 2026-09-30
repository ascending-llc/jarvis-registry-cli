package auth

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/ascending-llc/jarvis-registry-cli/cfg"
)

// LogoutCommand removes the configured Registry's locally cached credentials.
type LogoutCommand struct {
	userHomeDir    string
	registryDir    string
	baseUrl        string
	logger         Logger
	configLoadFunc func(string) (cfg.Config, error)
	exitFunc       func(int)
	resolver       RegistryTokenResolver
}

// BeforeReset supplies home, config, output and exit defaults before flag parsing.
func (c *LogoutCommand) BeforeReset() (err error) {
	if c.userHomeDir, err = os.UserHomeDir(); err != nil {
		return fmt.Errorf("could not locate user home directory: %s", err.Error())
	}

	c.configLoadFunc = cfg.Load
	c.logger = log.New(os.Stdout, "", 0)
	c.exitFunc = os.Exit

	return nil
}

// AfterApply builds the resolver using the configured Registry and storage mode.
func (c *LogoutCommand) AfterApply() error {
	c.registryDir = filepath.Join(c.userHomeDir, cfg.RegistryDirName)

	config, err := c.configLoadFunc(c.registryDir)
	if err != nil {
		return fmt.Errorf("failed to load config options: %s", err.Error())
	}

	c.baseUrl = config.Registry.BaseUrl
	c.resolver = NewRegistryTokenResolver(config.Registry.AuthBaseUrl, RegistryScopes, c.registryDir, config.Local.Credentials.FileOnly, c.logger)

	return nil
}

// Run deletes local credentials and reports the result without contacting the server.
// An absent login exits 1 without an additional kong error line.
func (c *LogoutCommand) Run() error {
	err := c.resolver.Logout()
	if errors.Is(err, ErrNotAuthenticated) {
		c.logger.Printf("✗ Not logged in to %s. Run `jarvis-registry auth login` to authenticate.\n", c.baseUrl)
		c.exitFunc(1)

		return nil
	}

	if err != nil {
		return fmt.Errorf("failed to log out of the Registry: %s", err.Error())
	}

	c.logger.Printf("✓ Logged out of %s\n", c.baseUrl)

	return nil
}
