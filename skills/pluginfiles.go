package skills

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const (
	// reservedSyncSkillsName is the name of this CLI's own wrapper skill.
	// No Registry skill may use it, case-insensitively (see
	// isSafeSkillName's caller in Run and cleanDestDir).
	reservedSyncSkillsName = "sync-skills"

	// managedByValue marks a skill-lock.json (and therefore its
	// containing plugin root) as owned by this CLI.
	managedByValue = "jarvis-registry-cli"

	// syncSkillsVersion is bumped by hand whenever
	// embedded/sync-skills-SKILL.md changes.
	syncSkillsVersion = 1
)

var (
	//go:embed embedded/plugin.json
	pluginManifestContent []byte

	//go:embed embedded/sync-skills-SKILL.md
	syncSkillsSkillContent []byte
)

// atomicWriteFile writes content to path by first writing a temp file in
// the same directory and renaming it into place: os.Rename is a single,
// same-filesystem, atomic syscall, so a crash mid-write can never leave a
// truncated or partially-written file at path.
func atomicWriteFile(path string, content []byte, perm os.FileMode) error {
	tempFile, err := os.CreateTemp(filepath.Dir(path), ".jarvis-registry-plugin-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file for %s: %s", path, err.Error())
	}

	tempPath := tempFile.Name()

	if _, err = tempFile.Write(content); err != nil {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)

		return fmt.Errorf("failed to write temp file for %s: %s", path, err.Error())
	}

	if err = tempFile.Close(); err != nil {
		_ = os.Remove(tempPath)

		return fmt.Errorf("failed to close temp file for %s: %s", path, err.Error())
	}

	if err = os.Chmod(tempPath, perm); err != nil {
		_ = os.Remove(tempPath)

		return fmt.Errorf("failed to set permissions on temp file for %s: %s", path, err.Error())
	}

	if err = os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)

		return fmt.Errorf("failed to move temp file into place at %s: %s", path, err.Error())
	}

	return nil
}

// reconcilePluginManifest ensures <pluginRoot>/.claude-plugin/plugin.json
// exists and matches pluginManifestContent exactly, atomically rewriting
// it (creating .claude-plugin/ first if needed) whenever it doesn't.
// created is true only when the file did not exist before this call —
// that is the one signal, per Claude Code's documented behavior ("loaded
// as a plugin ... on the next session"), that determines whether the
// "restart your Claude Code session" banner is needed. A one-line stderr
// notice is logged via stderrLogger only when overwriting
// existing-but-different content, not on first creation, since that's
// expected bootstrap rather than silent drift-correction.
func reconcilePluginManifest(pluginRoot string, stderrLogger Logger) (created bool, err error) {
	dir := filepath.Join(pluginRoot, ".claude-plugin")
	path := filepath.Join(dir, "plugin.json")

	existing, readErr := os.ReadFile(path)

	switch {
	case errors.Is(readErr, fs.ErrNotExist):
		created = true
	case readErr != nil:
		return false, fmt.Errorf("failed to read plugin manifest at %s: %s", path, readErr.Error())
	case bytes.Equal(existing, pluginManifestContent):
		return false, nil
	default:
		stderrLogger.Printf("%s exists but was not managed by this CLI's current content; overwriting it", path)
	}

	if err = os.MkdirAll(dir, 0755); err != nil {
		return false, fmt.Errorf("failed to create plugin manifest directory at %s: %s", dir, err.Error())
	}

	if err = atomicWriteFile(path, pluginManifestContent, 0644); err != nil {
		return false, err
	}

	return created, nil
}

// reconcileSyncSkillsWrapper rewrites <destDir>/sync-skills/SKILL.md from
// syncSkillsSkillContent (creating <destDir>/sync-skills/ first if
// needed — on a brand-new plugin root that folder does not exist yet)
// whenever the file is missing, unreadable, not a regular file
// (mirroring stageOne's stat check), or recordedVersion disagrees with
// the current syncSkillsVersion. recordedVersion is not this function's
// own concern — the caller reads it off the manifest
// (ManifestV1.SyncSkillsVersion) before invoking this. A one-line stderr
// notice is logged via stderrLogger only when an actual pre-existing
// version mismatch is being overwritten. Returns syncSkillsVersion on
// success, to be threaded into WriteManifest.
func reconcileSyncSkillsWrapper(destDir string, recordedVersion int, stderrLogger Logger) (newVersion int, err error) {
	dir := filepath.Join(destDir, reservedSyncSkillsName)
	path := filepath.Join(dir, "SKILL.md")

	var needChange bool

	if stat, statErr := os.Stat(path); errors.Is(statErr, fs.ErrNotExist) {
		needChange = true
	} else if statErr != nil {
		needChange = true
	} else if !stat.Mode().IsRegular() {
		needChange = true
	} else if recordedVersion != syncSkillsVersion {
		needChange = true

		stderrLogger.Printf("%s is out of date (recorded version %d, current version %d); rewriting it", path, recordedVersion, syncSkillsVersion)
	}

	if !needChange {
		return syncSkillsVersion, nil
	}

	if err = os.MkdirAll(dir, 0755); err != nil {
		return 0, fmt.Errorf("failed to create sync-skills wrapper directory at %s: %s", dir, err.Error())
	}

	if err = atomicWriteFile(path, syncSkillsSkillContent, 0644); err != nil {
		return 0, err
	}

	return syncSkillsVersion, nil
}
