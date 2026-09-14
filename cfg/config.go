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

		// Local holds settings that configure this machine's own CLI
		// behavior, as opposed to anything about the Registry server itself.
		Local struct {
			// Skills holds settings specific to the sync-skills subcommand.
			Skills struct {
				// SkipIds lists skill Ids that sync-skills must never create,
				// update, or keep synced locally, even when the caller has
				// Registry access.
				SkipIds []string `mapstructure:"skip_ids"`
			} `mapstructure:"skills"`
		} `mapstructure:"local"`
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
// Registry.BaseUrl and Local.Skills.SkipIds, and defaulting
// Registry.AuthBaseUrl to Registry.BaseUrl when it isn't set. It returns an
// error if neither file exists, the file cannot be parsed, or validation
// fails.
func Load(registryDir string) (config Config, err error) {
	v := viper.New()

	path, existed, resolveErr := resolveConfigPath(registryDir)
	if resolveErr == nil && !existed {
		return Config{}, fmt.Errorf("neither config.yaml nor config.yml exists in the %s folder", registryDir)
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

	config.Local.Skills.SkipIds, err = normalizeSkipIds(config.Local.Skills.SkipIds)
	if err != nil {
		return config, fmt.Errorf("invalid local.skills.skip_ids in %s: %s", path, err.Error())
	}

	return config, nil
}

// normalizeSkipIds trims surrounding whitespace from every configured skill
// Id and removes duplicates while preserving the first occurrence's position.
// It returns nil for an empty input and rejects entries that are empty after
// trimming.
func normalizeSkipIds(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	seen := make(map[string]struct{}, len(raw))
	normalized := make([]string, 0, len(raw))

	for i, id := range raw {
		id = strings.TrimSpace(id)
		if id == "" {
			return nil, fmt.Errorf("entry %d is empty", i)
		}

		if _, ok := seen[id]; ok {
			continue
		}

		seen[id] = struct{}{}
		normalized = append(normalized, id)
	}

	return normalized, nil
}

// resolveConfigPath returns the existing config.yaml or config.yml path in
// registryDir, preferring config.yaml. When neither exists, it returns the
// prospective config.yaml path with existed set to false.
func resolveConfigPath(registryDir string) (path string, existed bool, err error) {
	path = filepath.Join(registryDir, "config.yaml")

	if _, err = os.Stat(path); err == nil {
		return path, true, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return path, false, err
	}

	ymlPath := filepath.Join(registryDir, "config.yml")

	if _, err = os.Stat(ymlPath); err == nil {
		return ymlPath, true, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return ymlPath, false, err
	}

	return path, false, nil
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
