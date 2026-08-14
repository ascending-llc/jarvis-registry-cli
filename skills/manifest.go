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
	ManifestReadWriter struct {
		path string
	}

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

func NewManifestReadWriter(registryDir string) ManifestReadWriter {
	return ManifestReadWriter{path: filepath.Join(registryDir, manifestFileName)}
}

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

	if err = os.Chmod(mrw.path, 0644); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("failed to toggle manifest file %s writable: %s", mrw.path, err.Error())
	}

	if err = os.WriteFile(mrw.path, content, 0444); err != nil {
		return fmt.Errorf("failed to write manifest file at %s: %s", mrw.path, err.Error())
	}

	if err = os.Chmod(mrw.path, 0444); err != nil {
		return fmt.Errorf("failed to toggle manifest file %s read-only: %s", mrw.path, err.Error())
	}

	return nil
}
