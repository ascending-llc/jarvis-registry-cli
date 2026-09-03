package auth

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/ascending-llc/jarvis-registry-cli/cfg"
)

// StatusCommand implements the "auth status" subcommand: it reports
// whether a valid Registry access token is currently cached, without
// running the OAuth device flow.
type StatusCommand struct {
	userHomeDir    string
	registryDir    string
	baseUrl        string
	authBaseUrl    string
	logger         Logger
	configLoadFunc func(string) (cfg.Config, error)
	exitFunc       func(int)
	resolver       RegistryTokenResolver
}

// BeforeReset sets defaults for StatusCommand that don't depend on parsed
// flags: the user's home directory, the config loader, the logger, and the
// exit func.
func (c *StatusCommand) BeforeReset() (err error) {
	if c.userHomeDir, err = os.UserHomeDir(); err != nil {
		return fmt.Errorf("could not locate user home directory: %s", err.Error())
	}

	c.configLoadFunc = cfg.Load
	c.logger = log.New(os.Stdout, "", 0)
	c.exitFunc = os.Exit

	return nil
}

// AfterApply derives StatusCommand's remaining dependencies from the
// loaded config: the registry directory, Registry and auth-server base
// URLs, and token resolver.
func (c *StatusCommand) AfterApply() (err error) {
	c.registryDir = filepath.Join(c.userHomeDir, cfg.RegistryDirName)

	config, err := c.configLoadFunc(c.registryDir)
	if err != nil {
		return fmt.Errorf("failed to load config options: %s", err.Error())
	}

	c.baseUrl = config.Registry.BaseUrl
	c.authBaseUrl = config.Registry.AuthBaseUrl
	c.resolver = NewRegistryTokenResolver(c.authBaseUrl, RegistryScopes, c.logger)

	return nil
}

// Run reports the configured Registry base URL and current authentication
// status. When not logged in, it prints its own report and exits 1
// directly, returning nil so kong does not print a redundant error line
// after the report.
func (c *StatusCommand) Run() error {
	c.logger.Println(c.baseUrl)

	st, loggedIn, err := c.resolver.Status()
	if err != nil {
		return fmt.Errorf("failed to check Registry authentication status: %s", err.Error())
	}

	if !loggedIn {
		c.logger.Println("✗ Not logged in. Run `jarvis-registry auth login` to authenticate.")
		c.exitFunc(1)

		return nil
	}

	c.logger.Println("✓ Logged in (keyring)")
	c.logger.Printf("- Token scopes: %s\n", formatScopes(st.Scope))

	return nil
}

// formatScopes renders a space-separated OAuth scope string as a
// comma-separated, single-quoted list, e.g. "skills-read other-scope" ->
// "'skills-read', 'other-scope'", matching gh auth status's formatting.
func formatScopes(scope string) string {
	fields := strings.Fields(scope)
	quoted := make([]string, len(fields))

	for i, f := range fields {
		quoted[i] = "'" + f + "'"
	}

	return strings.Join(quoted, ", ")
}
