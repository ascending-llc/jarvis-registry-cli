package skills

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	junction "github.com/nyaosorg/go-windows-junction"

	"github.com/ascending-llc/jarvis-registry-cli/cfg"
)

type linkOutcome struct {
	Err    error
	Name   string
	Status string
}

const (
	codexUserScopeDirName   = ".codex"
	copilotUserScopeDirName = ".copilot"
	linkStatusLinked        = "Linked"
	linkStatusUnchanged     = "Unchanged"
	linkStatusRelinked      = "Relinked"
	linkStatusSkipped       = "Skipped"
	linkStatusRemoved       = "Removed"
	linkStatusFailed        = "Failed"

	// Readlink on an ordinary Windows file/directory returns
	// ERROR_NOT_A_REPARSE_POINT rather than POSIX EINVAL.
	errNotAReparsePoint = syscall.Errno(4390)
)

func personalScopeSyncRoot(registryDir string, mode cfg.SkillsMode) string {
	return filepath.Join(registryDir, "skills", string(mode))
}

func userScopeSkillsDir(userHomeDir string, mode cfg.SkillsMode) (string, error) {
	switch mode {
	case cfg.SkillsModeCodex:
		return filepath.Join(userHomeDir, codexUserScopeDirName, "skills"), nil
	case cfg.SkillsModeCopilot:
		return filepath.Join(userHomeDir, copilotUserScopeDirName, "skills"), nil
	case cfg.SkillsModeClaude:
	}

	return "", fmt.Errorf("personal skill links are not supported in %q mode", mode)
}

// readLinkTarget returns the cleaned absolute target of a symlink or
// junction, or an empty string for an ordinary file/directory. Inspection
// errors (including permission and I/O failures) are preserved so a
// collision is never treated as a recursively-deletable directory.
func readLinkTarget(link string) (string, error) {
	if _, err := os.Lstat(link); err != nil {
		return "", err
	}

	target, err := os.Readlink(link)
	if errors.Is(err, syscall.EINVAL) || errors.Is(err, errNotAReparsePoint) {
		return "", nil
	}

	if err != nil {
		return "", err
	}

	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(link), target)
	}

	return filepath.Abs(target)
}

// sameLinkPath reports whether two paths name the same location, first
// lexically and then by file identity when both exist (macOS /var vs
// /private/var, Windows casing).
func sameLinkPath(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}

	aInfo, aErr := os.Stat(a)
	bInfo, bErr := os.Stat(b)

	return aErr == nil && bErr == nil && os.SameFile(aInfo, bInfo)
}

func (c *SyncCommand) prepareUserScopeSkillsDir() (string, error) {
	dir, err := userScopeSkillsDir(c.userHomeDir, c.mode)
	if err != nil {
		return "", err
	}

	if err = os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create personal skills directory %s: %s", dir, err.Error())
	}

	return dir, nil
}

func (c *SyncCommand) reconcileSymlinks(names []string) ([]linkOutcome, error) {
	dir, err := c.prepareUserScopeSkillsDir()
	if err != nil {
		return nil, err
	}

	outcomes := make([]linkOutcome, 0, len(names))
	for _, name := range names {
		status, linkErr := c.reconcileSymlink(dir, name)
		outcomes = append(outcomes, newLinkOutcome(name, status, linkErr))
	}

	return outcomes, nil
}

func (c *SyncCommand) reconcileSymlink(dir, name string) (string, error) {
	target := filepath.Join(c.destDir, name)
	link := filepath.Join(dir, name)

	existing, err := readLinkTarget(link)
	if errors.Is(err, fs.ErrNotExist) {
		return linkStatusLinked, junction.Create(target, link)
	}

	if err != nil {
		return "", err
	}

	if existing != "" && sameLinkPath(existing, target) {
		return linkStatusUnchanged, nil
	}

	if !c.confirmLinkReplacement(link) {
		c.stderrLogger.Printf("Skipped link %s: an existing entry was left untouched", link)

		return linkStatusSkipped, nil
	}

	status := linkStatusLinked
	if existing != "" {
		// Remove only the entry, never the contents of its target.
		err = os.Remove(link)
		status = linkStatusRelinked
	} else {
		err = atomicRemoveAll(link)
	}

	if err != nil {
		return "", err
	}

	return status, junction.Create(target, link)
}

func (c *SyncCommand) confirmLinkReplacement(path string) bool {
	if !c.Interactive {
		return c.override
	}

	c.stderrLogger.Printf("Replace existing entry at %s? [y/N]", path)

	response, err := c.lineReader().ReadString('\n')
	if err != nil {
		return false
	}

	response = strings.TrimSpace(response)

	return strings.EqualFold(response, "y") || strings.EqualFold(response, "yes")
}

func (c *SyncCommand) lineReader() *bufio.Reader {
	if reader, ok := c.stdin.(*bufio.Reader); ok {
		return reader
	}

	reader := bufio.NewReader(c.stdin)
	c.stdin = reader

	return reader
}

func (c *SyncCommand) pruneDanglingLinks() ([]linkOutcome, error) {
	dir, err := c.prepareUserScopeSkillsDir()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to list personal skills directory %s: %s", dir, err.Error())
	}

	var outcomes []linkOutcome
	for _, entry := range entries {
		link := filepath.Join(dir, entry.Name())

		target, readErr := readLinkTarget(link)
		if errors.Is(readErr, fs.ErrNotExist) {
			continue
		}

		if readErr != nil {
			outcomes = append(outcomes, newLinkOutcome(entry.Name(), "", readErr))

			continue
		}

		if target == "" || !sameLinkPath(filepath.Dir(target), c.destDir) {
			continue
		}

		_, statErr := os.Stat(target)
		if statErr == nil {
			continue
		}

		if errors.Is(statErr, fs.ErrNotExist) {
			statErr = os.Remove(link)
			outcomes = append(outcomes, newLinkOutcome(entry.Name(), linkStatusRemoved, statErr))

			continue
		}

		outcomes = append(outcomes, newLinkOutcome(entry.Name(), "", statErr))
	}

	return outcomes, nil
}

// removeLegacyWrapperLink retires only a wrapper link made by older
// personal-scope syncs; a user's own same-named entry is left untouched.
func (c *SyncCommand) removeLegacyWrapperLink() error {
	dir, err := userScopeSkillsDir(c.userHomeDir, c.mode)
	if err != nil {
		return err
	}

	link := filepath.Join(dir, reservedSyncSkillsName)

	target, err := readLinkTarget(link)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	if err != nil || target == "" || !sameLinkPath(target, filepath.Join(c.destDir, reservedSyncSkillsName)) {
		return err
	}

	if err = os.Remove(link); err != nil {
		return fmt.Errorf("failed to remove legacy wrapper link %s: %s", link, err.Error())
	}

	return nil
}

func newLinkOutcome(name, status string, err error) linkOutcome {
	if err != nil {
		return linkOutcome{Name: name, Status: linkStatusFailed, Err: fmt.Errorf("skill %s link failed: %s", name, err.Error())}
	}

	return linkOutcome{Name: name, Status: status}
}

func joinLinkErrors(outcomes []linkOutcome) error {
	errs := make([]error, len(outcomes))
	for i, outcome := range outcomes {
		errs[i] = outcome.Err
	}

	return errors.Join(errs...)
}
