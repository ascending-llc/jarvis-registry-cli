package skills

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/ascending-llc/jarvis-registry-cli/cfg"
)

const (
	// reservedSyncSkillsName is the name of this CLI's own wrapper skill.
	// No Registry skill may use it, case-insensitively (see
	// isSafeSkillName's caller in Run and cleanDestDir).
	reservedSyncSkillsName = "sync-skills"

	// managedByValue marks a skill-lock.json (and therefore its
	// containing plugin root) as owned by this CLI.
	managedByValue = "jarvis-registry-cli"

	// syncSkillsVersion is bumped by hand whenever any of the three
	// embedded/sync-skills-*-SKILL.md files change.
	syncSkillsVersion = 9
)

var (
	//go:embed embedded/plugin.json
	pluginManifestContent []byte

	//go:embed embedded/sync-skills-claude-SKILL.md
	syncSkillsSkillContentClaude []byte

	//go:embed embedded/sync-skills-codex-SKILL.md
	syncSkillsSkillContentCodex []byte

	//go:embed embedded/sync-skills-copilot-SKILL.md
	syncSkillsSkillContentCopilot []byte
)

// renderPluginManifest adds the Registry MCP connection to the embedded plugin
// metadata. Only a deployment origin is accepted: credentials and URL suffixes
// must never be copied into the distributed manifest. Parse errors deliberately
// omit their cause because net/url errors can repeat the input URL.
func renderPluginManifest(registryOrigin string) ([]byte, error) {
	u, err := url.Parse(registryOrigin)
	if err != nil {
		return nil, errors.New("invalid registry.base_url for Claude Code: expected a deployment origin")
	}

	localHTTP := u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1")
	if (u.Scheme != "https" && !localHTTP) || u.Hostname() == "" ||
		u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.Contains(registryOrigin, "#") ||
		(u.Path != "" && u.Path != "/") || u.RawPath != "" {
		return nil, errors.New("invalid registry.base_url for Claude Code: use an HTTPS origin (or HTTP localhost/127.0.0.1) without credentials, a path, query, or fragment")
	}

	u.Path = registryBasePath + "/proxy/mcpgw/mcp"

	var manifest map[string]any

	if err = json.Unmarshal(pluginManifestContent, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse embedded plugin manifest: %s", err.Error())
	}

	manifest["mcpServers"] = map[string]map[string]string{
		"jarvis-registry": {"type": "http", "url": u.String()},
	}

	content, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to render plugin manifest: %s", err.Error())
	}

	return append(content, '\n'), nil
}

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
// exists and matches content exactly, atomically rewriting
// it (creating .claude-plugin/ first if needed) whenever it doesn't.
// created identifies first-time delivery; changed reports any successful write,
// including an upgrade or Registry URL change that requires a plugin reload.
func reconcilePluginManifest(pluginRoot string, content []byte, stderrLogger Logger) (created, changed bool, err error) {
	dir := filepath.Join(pluginRoot, ".claude-plugin")
	path := filepath.Join(dir, "plugin.json")

	existing, readErr := os.ReadFile(path)

	switch {
	case errors.Is(readErr, fs.ErrNotExist):
		created = true
	case readErr != nil:
		return false, false, fmt.Errorf("failed to read plugin manifest at %s: %s", path, readErr.Error())
	case bytes.Equal(existing, content):
		return false, false, nil
	default:
		stderrLogger.Printf("%s exists but was not managed by this CLI's current content; overwriting it", path)
	}

	if err = os.MkdirAll(dir, 0755); err != nil {
		return false, false, fmt.Errorf("failed to create plugin manifest directory at %s: %s", dir, err.Error())
	}

	if err = atomicWriteFile(path, content, 0644); err != nil {
		return false, false, err
	}

	return created, true, nil
}

// reconcileSyncSkillsWrapper rewrites <destDir>/sync-skills/SKILL.md from
// the mode-specific embedded wrapper content (see wrapperContentForMode),
// creating <destDir>/sync-skills/ first if needed — on a brand-new sync
// root that folder does not exist yet —
// whenever the file is missing, unreadable, not a regular file
// (mirroring stageOne's stat check), or recordedVersion disagrees with
// the current syncSkillsVersion. recordedVersion is not this function's
// own concern — the caller reads it off the manifest
// (ManifestV1.SyncSkillsVersion) before invoking this. A one-line stderr
// notice is logged via stderrLogger only when an actual pre-existing
// version mismatch is being overwritten. Returns syncSkillsVersion on
// success, to be threaded into WriteManifest.
func reconcileSyncSkillsWrapper(destDir string, recordedVersion int, mode cfg.SkillsMode, stderrLogger Logger) (newVersion int, err error) {
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

	if err = atomicWriteFile(path, wrapperContentForMode(mode), 0644); err != nil {
		return 0, err
	}

	return syncSkillsVersion, nil
}

// wrapperContentForMode returns the embedded sync-skills wrapper SKILL.md
// content for mode.
func wrapperContentForMode(mode cfg.SkillsMode) []byte {
	switch mode {
	case cfg.SkillsModeCodex:
		return syncSkillsSkillContentCodex
	case cfg.SkillsModeCopilot:
		return syncSkillsSkillContentCopilot
	case cfg.SkillsModeClaude:
	}

	return syncSkillsSkillContentClaude
}
