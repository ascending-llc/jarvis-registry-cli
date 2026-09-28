// Package update implements checksum-verified CLI self-updates from GitHub releases.
package update

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/alecthomas/kong"
	"github.com/creativeprojects/go-selfupdate"

	"github.com/ascending-llc/jarvis-registry-cli/cfg"
)

type (
	// Logger is the minimal logging interface required by Command.
	Logger interface {
		// Print writes a message.
		Print(v ...any)
		// Printf writes a formatted message.
		Printf(format string, v ...any)
		// Println writes a message followed by a newline.
		Println(v ...any)
	}

	// Updater discovers releases and installs their verified executable.
	Updater interface {
		// DetectLatest finds the latest release matching the current platform.
		DetectLatest(ctx context.Context, repo selfupdate.Repository) (*selfupdate.Release, bool, error)
		// UpdateTo verifies and installs a release at the executable path.
		UpdateTo(ctx context.Context, release *selfupdate.Release, cmdPath string) error
	}

	// Command implements "update": check for or install a newer CLI release.
	Command struct { //nolint:govet // fieldalignment: keep the public CLI flag before its injected dependencies.
		// Check reports an available version without installing it.
		Check bool `help:"Report whether a newer version is available, without installing it."`

		logger             Logger
		stderrLogger       Logger
		executablePathFunc func() (string, error)
		updater            Updater
		currentVersion     string
		registryDir        string
	}
)

// BeforeReset initializes logging, executable resolution, the state directory,
// and the GitHub updater without loading Registry configuration.
// Constructing the updater does not make network requests.
func (c *Command) BeforeReset() error {
	c.logger = log.New(os.Stdout, "", 0)
	c.stderrLogger = log.New(os.Stderr, "", 0)
	c.executablePathFunc = selfupdate.ExecutablePath

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("could not locate user home directory: %s", err.Error())
	}

	c.registryDir = filepath.Join(home, cfg.RegistryDirName)

	source, err := selfupdate.NewGitHubSource(selfupdate.GitHubConfig{})
	if err != nil {
		return fmt.Errorf("could not initialize GitHub release source: %s", err.Error())
	}

	c.updater, err = newUpdater(source)
	if err != nil {
		return fmt.Errorf("could not initialize updater: %s", err.Error())
	}

	return nil
}

// AfterApply obtains the build version already supplied to Kong by main.
func (c *Command) AfterApply(vars kong.Vars) error {
	c.currentVersion = vars["version"]

	return nil
}

// Run checks for a newer release and optionally installs it at the resolved
// executable path. Development, Homebrew, and winget installs are rejected before I/O
// against GitHub, and newer local versions are never downgraded.
func (c *Command) Run() error {
	if c.currentVersion == "dev" {
		return fmt.Errorf("cannot self-update a dev build; reinstall with go install github.com/ascending-llc/jarvis-registry-cli/cmd/jarvis-registry@latest, or install a release binary")
	}

	version, err := semver.NewVersion(c.currentVersion)
	if err != nil {
		return fmt.Errorf("cannot self-update with invalid version %q; reinstall a tagged release: %s", c.currentVersion, err.Error())
	}

	exe, err := c.executablePathFunc()
	if err != nil {
		return fmt.Errorf("could not resolve the running executable: %s", err.Error())
	}

	if isHomebrewPath(exe) {
		return fmt.Errorf("this executable is managed by Homebrew; run brew upgrade jarvis-registry")
	}

	if isWingetPath(exe) {
		return fmt.Errorf("this executable is managed by winget; run winget upgrade Ascending.JarvisRegistryCLI")
	}

	ctx := context.Background()

	release, found, err := c.updater.DetectLatest(ctx, selfupdate.NewRepositorySlug("ascending-llc", "jarvis-registry-cli"))
	if err != nil {
		return fmt.Errorf("could not check for a CLI release: %s", err.Error())
	}

	if !found || release == nil {
		return fmt.Errorf("no compatible CLI release found in ascending-llc/jarvis-registry-cli")
	}

	if release.Equal(version.String()) {
		c.logger.Printf("jarvis-registry %s is already up to date.\n", c.currentVersion)

		return nil
	}

	if release.LessThan(version.String()) {
		c.logger.Printf("Current version %s is newer than release %s; no downgrade performed.\n", c.currentVersion, release.Version())

		return nil
	}

	if c.Check {
		c.logger.Printf("jarvis-registry %s is available (current: %s). Run jarvis-registry update to install it.\n", release.Version(), c.currentVersion)

		return nil
	}

	return c.installRelease(ctx, release, exe)
}

func (c *Command) installRelease(ctx context.Context, release *selfupdate.Release, exe string) error {
	lock, err := acquireUpdateLock(c.registryDir, exe)
	if err != nil {
		return err
	}

	defer func() {
		if err := lock.Close(); err != nil {
			c.stderrLogger.Printf("Could not release update lock for %q: %s\n", exe, err.Error())
		}
	}()

	if err := c.updater.UpdateTo(ctx, release, exe); err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return fmt.Errorf("could not update %q (check write permissions on its directory): %s", exe, err.Error())
		}

		return fmt.Errorf("could not update %q: %s", exe, err.Error())
	}

	c.logger.Printf("Updated jarvis-registry to %s.\n", release.Version())

	return nil
}

func newUpdater(source selfupdate.Source) (*selfupdate.Updater, error) {
	return selfupdate.NewUpdater(selfupdate.Config{
		Source:    source,
		Validator: &selfupdate.ChecksumValidator{UniqueFilename: "checksums.txt"},
	})
}

func isHomebrewPath(exe string) bool {
	exe = strings.ToLower(filepath.ToSlash(exe))

	return strings.Contains(exe, "/cellar/") || strings.Contains(exe, "/homebrew/") || strings.Contains(exe, "/linuxbrew/")
}

func isWingetPath(exe string) bool {
	exe = strings.ToLower(filepath.ToSlash(exe))

	return strings.Contains(exe, "/winget/packages/")
}
