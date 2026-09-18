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
			// Skills holds settings specific to the skills sync subcommand.
			Skills struct { //nolint:govet // fieldalignment: keep SkipIds, Mode, and Link in the order they were introduced, each with its own doc comment, rather than let the fixer collapse them into an undocumented, alignment-packed block.
				// SkipIds lists skill Ids that skills sync must never create,
				// update, or keep synced locally, even when the caller has
				// Registry access.
				SkipIds []string `mapstructure:"skip_ids"`

				// Mode selects the skills-directory convention skills sync
				// targets. Empty means unset — cfg.Load does not default it,
				// so skills sync can distinguish "never configured" from any
				// of the three real modes and fail with an actionable message
				// naming exactly what's missing.
				Mode SkillsMode `mapstructure:"mode"`

				// Link controls collisions when personal-scope Codex/Copilot
				// sync creates same-named entries in the tool's own skills
				// directory. Not prompted by `jarvis-registry configure`.
				Link struct {
					// Override, when true, lets personal-scope Codex/Copilot
					// symlink reconciliation replace an existing
					// file/folder/symlink at a still-desired skill name.
					// Default false: a collision is left untouched and logged.
					Override bool `mapstructure:"override"`
				} `mapstructure:"link"`
			} `mapstructure:"skills"`
		} `mapstructure:"local"`
	}

	// SkillsMode selects which skills-directory convention skills sync
	// targets.
	SkillsMode string
)

const (
	// SkillsModeClaude syncs into Claude Code's plugin-owned subtree at
	// <ProjectPath>/.claude/skills/jarvis-registry/skills/.
	SkillsModeClaude SkillsMode = "claude"

	// SkillsModeCodex syncs into <ProjectPath>/.agents/skills/, or a
	// CLI-owned personal root at ~/.jarvis-registry/skills/codex/ with
	// links under ~/.codex/skills/ when no path is given.
	SkillsModeCodex SkillsMode = "codex"

	// SkillsModeCopilot syncs into <ProjectPath>/.github/skills/, or a
	// CLI-owned personal root at ~/.jarvis-registry/skills/copilot/ with
	// links under ~/.copilot/skills/ when no path is given.
	SkillsModeCopilot SkillsMode = "copilot"

	// RegistryDirName is the name of the per-user directory, under the
	// user's home directory, that holds the CLI's config file and its
	// advisory sync locks (see skills.acquireLock), plus personal
	// Codex/Copilot skill roots. Each manifest lives inside the resolved
	// sync root.
	RegistryDirName = ".jarvis-registry"
)

// Valid reports whether m is one of the three recognized modes. The zero
// value ("") is not valid — an unset mode is a distinct, caller-visible
// state, not a fourth mode.
func (m SkillsMode) Valid() bool {
	switch m {
	case SkillsModeClaude, SkillsModeCodex, SkillsModeCopilot:
		return true
	default:
		return false
	}
}

// Load reads config.yaml (or config.yml) from registryDir, unmarshals it
// into a Config, and validates and resolves its fields — normalizing
// Registry.BaseUrl and Local.Skills.SkipIds, defaulting Registry.AuthBaseUrl
// to Registry.BaseUrl when it isn't set, and validating Local.Skills.Mode
// when it's set. It returns an error if neither file exists, the file
// cannot be parsed, or validation fails.
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

	// Validate the skills mode only when it's set: a config file predating
	// this field (or one deliberately leaving it to be supplied via --mode)
	// must still load. No default is applied — an absent mode stays absent
	// and is handled by skills sync's own fail-loud resolution.
	if config.Local.Skills.Mode != "" && !config.Local.Skills.Mode.Valid() {
		return config, fmt.Errorf("invalid local.skills.mode in %s: must be one of claude, codex, copilot, got %q", path, config.Local.Skills.Mode)
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
