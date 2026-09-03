package skills

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

type (
	// ManifestReadWriter reads and writes the local sync manifest file
	// that records which skills were last synced to the destination
	// folder.
	ManifestReadWriter struct {
		path string
	}

	// ManifestV1 is the on-disk schema of the sync manifest file.
	ManifestV1 struct {
		LastSyncedAt      time.Time  `json:"lastSyncedAt,omitzero"`
		Description       string     `json:"description"`
		ManagedBy         string     `json:"managedBy"`
		Skills            []Metadata `json:"skills"`
		SchemaVersion     int        `json:"schemaVersion"`
		SyncSkillsVersion int        `json:"syncSkillsVersion"`
	}
)

const (
	manifestFileName = "skill-lock.json"

	manifestSchemaVersion = 1

	manifestDescription = "Manifest file for Jarvis Registry skill sync-down."
)

// NewManifestReadWriter returns a ManifestReadWriter for the manifest file
// inside pluginRoot.
func NewManifestReadWriter(pluginRoot string) ManifestReadWriter {
	return ManifestReadWriter{path: filepath.Join(pluginRoot, manifestFileName)}
}

// ReadManifest returns the manifest file's contents, or a zero ManifestV1
// if the file does not exist or its content is malformed. A corrupt
// manifest and a missing one are treated identically: once past the
// consent gate (see ensurePluginRootConsent), neither leaves a
// trustworthy record of a prior sync, and Run resyncs every skill fresh
// from the Registry in that situation regardless. A read failure (e.g.
// the file exists but a permission error prevents reading it) is still a
// hard error, since that indicates a real environment issue self-healing
// can't fix.
func (mrw ManifestReadWriter) ReadManifest() (ManifestV1, error) {
	var m ManifestV1

	var err error

	if _, err = os.Stat(mrw.path); errors.Is(err, fs.ErrNotExist) {
		return ManifestV1{}, nil
	}

	var content []byte

	if content, err = os.ReadFile(mrw.path); err != nil {
		return ManifestV1{}, fmt.Errorf("manifest file exists at %s but cannot be read: %s", mrw.path, err.Error())
	}

	if err = json.Unmarshal(content, &m); err != nil {
		return ManifestV1{}, nil
	}

	return m, nil
}

// WriteManifest overwrites the manifest file with skills, stamping
// ManagedBy, a fresh LastSyncedAt, and syncSkillsVersion, and marking the
// file read-only afterward. It writes to a temp file in the same
// directory first and renames it into place: os.Rename is a single,
// same-filesystem, atomic syscall, so a crash mid-write can never leave
// a truncated or partially-written manifest at mrw.path. Renaming onto
// an existing target does not require that target to be writable on
// POSIX, since rename relinks a directory entry rather than opening the
// target for writing.
func (mrw ManifestReadWriter) WriteManifest(skills []Metadata, syncSkillsVersion int) error {
	m := ManifestV1{
		SchemaVersion:     manifestSchemaVersion,
		Description:       manifestDescription,
		Skills:            skills,
		ManagedBy:         managedByValue,
		LastSyncedAt:      time.Now().UTC(),
		SyncSkillsVersion: syncSkillsVersion,
	}

	content, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("failed to marshal manifest file contents: %s", err.Error())
	}

	tempFile, err := os.CreateTemp(filepath.Dir(mrw.path), ".skill-lock-*.json.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file for manifest write: %s", err.Error())
	}

	tempPath := tempFile.Name()

	if _, err = tempFile.Write(content); err != nil {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)

		return fmt.Errorf("failed to write temp manifest file at %s: %s", tempPath, err.Error())
	}

	if err = tempFile.Close(); err != nil {
		_ = os.Remove(tempPath)

		return fmt.Errorf("failed to close temp manifest file at %s: %s", tempPath, err.Error())
	}

	if err = os.Chmod(tempPath, 0444); err != nil {
		_ = os.Remove(tempPath)

		return fmt.Errorf("failed to toggle temp manifest file %s read-only: %s", tempPath, err.Error())
	}

	if err = os.Rename(tempPath, mrw.path); err != nil {
		_ = os.Remove(tempPath)

		return fmt.Errorf("failed to move temp manifest file into place at %s: %s", mrw.path, err.Error())
	}

	return nil
}
