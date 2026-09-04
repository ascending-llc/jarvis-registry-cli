package cfg

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
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
	}
)

// RegistryDirName is the name of the per-user directory, under the user's
// home directory, that holds the CLI's config file and its advisory sync
// locks (see skills.acquireLock). The sync manifest itself lives inside
// the plugin root that skills.SyncCommand derives from its ProjectPath
// argument, not here.
const RegistryDirName = ".jarvis-registry"

// Load reads config.yaml (or config.yml) from registryDir, unmarshals it
// into a Config, and validates and resolves its fields — normalizing
// Registry.BaseUrl and defaulting Registry.AuthBaseUrl to Registry.BaseUrl
// when it isn't set. It returns an error if neither file exists, the file
// cannot be parsed, or validation fails.
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
