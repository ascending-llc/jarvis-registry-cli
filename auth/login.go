package auth

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/ascending-llc/jarvis-registry-cli/cfg"
)

// LoginCommand implements the "auth login" subcommand: it ensures a valid
// Registry access token is cached in the OS keyring, running the OAuth
// device flow if necessary.
type LoginCommand struct {
	userHomeDir    string
	registryDir    string
	authBaseUrl    string
	logger         Logger
	configLoadFunc func(string) (cfg.Config, error)
	resolver       RegistryTokenResolver
}

// BeforeReset sets defaults for LoginCommand that don't depend on parsed
// flags: the user's home directory, the config loader, and the logger.
func (c *LoginCommand) BeforeReset() (err error) {
	if c.userHomeDir, err = os.UserHomeDir(); err != nil {
		return fmt.Errorf("could not locate user home directory: %s", err.Error())
	}

	c.configLoadFunc = cfg.Load
	c.logger = log.New(os.Stdout, "", 0)

	return nil
}

// AfterApply derives LoginCommand's remaining dependencies from the loaded
// config: the registry directory, auth-server base URL, and token
// resolver.
func (c *LoginCommand) AfterApply() (err error) {
	c.registryDir = filepath.Join(c.userHomeDir, cfg.RegistryDirName)

	config, err := c.configLoadFunc(c.registryDir)
	if err != nil {
		return fmt.Errorf("failed to load config options: %s", err.Error())
	}

	c.authBaseUrl = config.Registry.AuthBaseUrl
	c.resolver = NewRegistryTokenResolver(c.authBaseUrl, RegistryScopes, c.logger)

	return nil
}

// Run ensures a valid Registry access token is cached, running the OAuth
// device flow if no cached credentials exist and refreshing fails.
func (c *LoginCommand) Run() error {
	if _, err := c.resolver.Login(); err != nil {
		return fmt.Errorf("failed to log in to the Registry: %s", err.Error())
	}

	c.logger.Println("✓ Logged in")

	return nil
}
