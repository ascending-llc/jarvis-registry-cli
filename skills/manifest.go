package skills

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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
		Description   string     `json:"description"`
		Skills        []Metadata `json:"skills"`
		SchemaVersion int        `json:"schemaVersion"`
	}
)

const (
	manifestFileName = "skill-lock.json"

	manifestSchemaVersion = 1

	manifestDescription = "Manifest file for Jarvis Registry skill sync-down."
)

// NewManifestReadWriter returns a ManifestReadWriter for the manifest file
// inside registryDir.
func NewManifestReadWriter(registryDir string) ManifestReadWriter {
	return ManifestReadWriter{path: filepath.Join(registryDir, manifestFileName)}
}

// ReadSkills returns the skills recorded in the manifest file, or nil if
// the file does not exist.
func (mrw ManifestReadWriter) ReadSkills() ([]Metadata, error) {
	var m ManifestV1

	var err error

	if _, err = os.Stat(mrw.path); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	var content []byte

	if content, err = os.ReadFile(mrw.path); err != nil {
		return nil, fmt.Errorf("manifest file exists at %s but cannot be read: %s", mrw.path, err.Error())
	}

	if err = json.Unmarshal(content, &m); err != nil {
		return nil, fmt.Errorf("failed to unmarshal manifest file at %s: %s", mrw.path, err.Error())
	}

	return m.Skills, nil
}

// WriteManifest overwrites the manifest file with skills, marking the
// file read-only afterward. It writes to a temp file in the same
// directory first and renames it into place: os.Rename is a single,
// same-filesystem, atomic syscall, so a crash mid-write can never leave
// a truncated or partially-written manifest at mrw.path. Renaming onto
// an existing target does not require that target to be writable on
// POSIX, since rename relinks a directory entry rather than opening the
// target for writing.
func (mrw ManifestReadWriter) WriteManifest(skills []Metadata) error {
	m := ManifestV1{
		SchemaVersion: manifestSchemaVersion,
		Description:   manifestDescription,
		Skills:        skills,
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
