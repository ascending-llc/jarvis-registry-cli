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
	Config struct {
		Registry struct {
			BaseUrl string `mapstructure:"base_url"`
		} `mapstructure:"registry"`

		Local struct {
			Dest string `mapstructure:"destination_folder"`
		} `mapstructure:"local"`
	}
)

// homeTokenPattern matches literal $HOME / $USERPROFILE references, in either
// "$NAME", "${NAME}", or Windows "%NAME%" form. Matching is intentionally not
// tied to runtime.GOOS: a config file should resolve the same way regardless
// of which OS happens to be running it.
var homeTokenPattern = regexp.MustCompile(`\$(?:HOME|USERPROFILE)\b|\$\{(?:HOME|USERPROFILE)\}|%USERPROFILE%`)

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

	if config.Local.Dest, err = resolveDest(config.Local.Dest); err != nil {
		return config, fmt.Errorf("invalid local.destination_folder in %s: %s", path, err.Error())
	}

	config.Registry.BaseUrl = strings.TrimSuffix(config.Registry.BaseUrl, "/")

	if err = validateBaseUrl(config.Registry.BaseUrl); err != nil {
		return config, fmt.Errorf("invalid registry.base_url in %s: %s", path, err.Error())
	}

	return config, nil
}

// resolveDest expands a leading "~" and any literal $HOME / $USERPROFILE
// reference in dest to the current user's home directory, then validates
// that the result is safe to use as a sync destination. Sync deletes
// anything inside this folder that isn't a tracked skill, so the resolved
// path must be an unambiguous, absolute location that is not the home
// directory or a filesystem root.
func resolveDest(dest string) (string, error) {
	dest = strings.TrimSpace(dest)

	if dest == "" {
		return "", errors.New("path must not be empty")
	}

	if strings.ContainsRune(dest, 0) {
		return "", errors.New("path must not contain a NUL byte")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not locate user home directory: %s", err.Error())
	}

	switch {
	case dest == "~":
		dest = home
	case strings.HasPrefix(dest, "~/"), strings.HasPrefix(dest, `~\`):
		dest = filepath.Join(home, dest[2:])
	case strings.HasPrefix(dest, "~"):
		return "", errors.New(`"~user" home directories are not supported, only "~" for the current user`)
	}

	dest = homeTokenPattern.ReplaceAllString(dest, home)

	dest = filepath.Clean(dest)

	if !filepath.IsAbs(dest) {
		return "", fmt.Errorf("path %q must be absolute, or start with \"~\", \"$HOME\", or \"%%USERPROFILE%%\"", dest)
	}

	if dest == home || dest == filepath.Dir(dest) {
		return "", fmt.Errorf("path %q is unsafe: it resolves to the home directory or a filesystem root, and sync deletes anything inside it that isn't a known skill", dest)
	}

	return dest, nil
}

// validateBaseUrl requires raw to be a well-formed URL with an https scheme
// and a non-empty host.
func validateBaseUrl(raw string) error {
	if raw == "" {
		return errors.New("URL must not be empty")
	}

	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("not a valid URL: %s", err.Error())
	}

	if u.Scheme != "https" {
		return fmt.Errorf("scheme must be https, got %q", u.Scheme)
	}

	if u.Host == "" {
		return errors.New("URL must include a host")
	}

	return nil
}
