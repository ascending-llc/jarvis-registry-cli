package cfg

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/viper"
)

type (
	// Config is the CLI's on-disk configuration, loaded by Load from
	// config.yaml or config.yml in the Registry directory.
	Config struct {
		Registry struct {
			BaseUrl string `mapstructure:"base_url"`

			// AuthBaseUrl is the origin the CLI performs the OAuth device/
			// token flow against. It defaults to BaseUrl, which is correct
			// whenever the Registry API and its auth-server share a single
			// origin (as they do in every non-local deployment, fronted by
			// the same ALB). It only needs to be set explicitly when they
			// don't — e.g. local development, where the auth-server and the
			// Registry API listen on different localhost ports.
			AuthBaseUrl string `mapstructure:"auth_base_url"`
		} `mapstructure:"registry"`

		Local struct {
			// ProjectPath is the root directory under which this CLI
			// places its Claude Code skills-directory plugin, at
			// <ProjectPath>/.claude/skills/jarvis-registry/. Optional;
			// when unset, defaults to the user's home directory
			// (personal scope, loads in every project). Set it to a
			// repository root for a project-scope plugin instead.
			ProjectPath string `mapstructure:"project_path"`

			// PluginRoot is <ProjectPath>/.claude/skills/jarvis-registry/,
			// derived by Load. Not read from config.yaml.
			PluginRoot string `mapstructure:"-"`

			// Dest is <PluginRoot>/skills/, the folder sync-skills
			// reconciles Registry skills into. Derived by Load. Not
			// read from config.yaml.
			Dest string `mapstructure:"-"`
		} `mapstructure:"local"`
	}
)

// RegistryDirName is the name of the per-user directory, under the user's
// home directory, that holds the CLI's config file and its advisory sync
// locks (see skills.acquireLock). The sync manifest itself lives inside
// the plugin root (Config.Local.PluginRoot), not here.
const RegistryDirName = ".jarvis-registry"

// homeTokenPattern matches literal $HOME / $USERPROFILE references, in either
// "$NAME", "${NAME}", or Windows "%NAME%" form. Matching is intentionally not
// tied to runtime.GOOS: a config file should resolve the same way regardless
// of which OS happens to be running it.
var homeTokenPattern = regexp.MustCompile(`\$(?:HOME|USERPROFILE)\b|\$\{(?:HOME|USERPROFILE)\}|%USERPROFILE%`)

// Load reads config.yaml (or config.yml) from registryDir, unmarshals it
// into a Config, and validates and resolves its fields — expanding
// Local.ProjectPath to an absolute path and deriving Local.PluginRoot and
// Local.Dest from it, normalizing Registry.BaseUrl, and defaulting
// Registry.AuthBaseUrl to Registry.BaseUrl when it isn't set. It returns an
// error if neither file exists, the file cannot be parsed, or validation
// fails.
func Load(registryDir string) (config Config, err error) {
	v := viper.New()

	path := filepath.Join(registryDir, "config.yaml")

	if _, err = os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		path = filepath.Join(registryDir, "config.yml")

		if _, err = os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			return Config{}, fmt.Errorf("neither config.yaml nor config.yml exists in the %s folder", registryDir)
		}
	}

	v.SetConfigFile(path)

	if err = v.ReadInConfig(); err != nil {
		return config, fmt.Errorf("failed to read config file at %s: %s", path, err.Error())
	}

	if err = v.Unmarshal(&config); err != nil {
		return config, fmt.Errorf("failed to parse config file at %s: %s", path, err.Error())
	}

	if config.Local.ProjectPath, err = resolveProjectPath(config.Local.ProjectPath); err != nil {
		return config, fmt.Errorf("invalid local.project_path in %s: %s", path, err.Error())
	}

	config.Local.PluginRoot = filepath.Join(config.Local.ProjectPath, ".claude", "skills", "jarvis-registry")
	config.Local.Dest = filepath.Join(config.Local.PluginRoot, "skills")

	config.Registry.BaseUrl = strings.TrimSuffix(config.Registry.BaseUrl, "/")

	if err = validateBaseUrl(config.Registry.BaseUrl); err != nil {
		return config, fmt.Errorf("invalid registry.base_url in %s: %s", path, err.Error())
	}

	config.Registry.AuthBaseUrl = strings.TrimSuffix(config.Registry.AuthBaseUrl, "/")

	if config.Registry.AuthBaseUrl == "" {
		config.Registry.AuthBaseUrl = config.Registry.BaseUrl
	} else if err = validateBaseUrl(config.Registry.AuthBaseUrl); err != nil {
		return config, fmt.Errorf("invalid registry.auth_base_url in %s: %s", path, err.Error())
	}

	return config, nil
}

// resolveProjectPath expands a leading "~" and any literal $HOME /
// $USERPROFILE reference in dest to the current user's home directory,
// then validates that the result is an unambiguous, absolute location.
// An empty dest defaults to the home directory, so the plugin loads in
// personal scope by default.
func resolveProjectPath(dest string) (string, error) {
	dest = strings.TrimSpace(dest)

	if strings.ContainsRune(dest, 0) {
		return "", errors.New("path must not contain a NUL byte")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not locate user home directory: %s", err.Error())
	}

	switch {
	case dest == "":
		dest = home
	case dest == "~":
		dest = home
	case strings.HasPrefix(dest, "~/"), strings.HasPrefix(dest, `~\`):
		dest = filepath.Join(home, dest[2:])
	case strings.HasPrefix(dest, "~"):
		return "", errors.New(`"~user" home directories are not supported, only "~" for the current user`)
	}

	dest = homeTokenPattern.ReplaceAllLiteralString(dest, home)

	dest = filepath.Clean(dest)

	if !filepath.IsAbs(dest) {
		return "", fmt.Errorf("path %q must be absolute, or start with \"~\", \"$HOME\", or \"%%USERPROFILE%%\"", dest)
	}

	return dest, nil
}

// validateBaseUrl requires raw to be a well-formed URL with an https scheme
// and a non-empty host. As an exception for local testing, http is also
// accepted when the host is "localhost" or "127.0.0.1", at any port.
func validateBaseUrl(raw string) error {
	if raw == "" {
		return errors.New("URL must not be empty")
	}

	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("not a valid URL: %s", err.Error())
	}

	isLocalHTTP := u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1")
	if u.Scheme != "https" && !isLocalHTTP {
		return fmt.Errorf("scheme must be https, or http for localhost/127.0.0.1, got %q", u.Scheme)
	}

	if u.Host == "" {
		return errors.New("URL must include a host")
	}

	return nil
}
