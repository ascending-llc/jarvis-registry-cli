package creds

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ascending-llc/jarvis-registry-cli/internal/lockfile"
)

// FileReadWriter stores credentials in a plaintext JSON file, keyed by service.
// Writes replace the file with a 0600 temporary file under an advisory lock.
type FileReadWriter struct {
	path    string
	service string
}

// NewFileReadWriter returns a file store for one service at path.
func NewFileReadWriter(path, service string) FileReadWriter {
	return FileReadWriter{path: path, service: service}
}

// Read returns this service's credentials, or ErrCredentialsNotExist when absent.
func (rw FileReadWriter) Read() ([]byte, error) {
	entries, err := rw.readMap()
	if err != nil {
		return nil, err
	}

	content, ok := entries[rw.service]
	if !ok {
		return nil, fmt.Errorf("%w: path=%s service=%s", ErrCredentialsNotExist, rw.path, rw.service)
	}

	return []byte(content), nil
}

// Write replaces this service's credentials while preserving other entries.
// Failures wrap ErrCredentialWriteFailure.
func (rw FileReadWriter) Write(content []byte) error {
	err := rw.update(func(entries map[string]string) error {
		entries[rw.service] = string(content)

		return nil
	})
	if err != nil {
		return fmt.Errorf("%w: path=%s: %s", ErrCredentialWriteFailure, rw.path, err.Error())
	}

	return nil
}

// Delete removes this service's credentials, removing the file when empty.
// An absent entry returns ErrCredentialsNotExist.
func (rw FileReadWriter) Delete() error {
	// A cache miss must not create directories or require write access.
	if _, err := rw.Read(); err != nil {
		return err
	}

	return rw.update(func(entries map[string]string) error {
		if _, ok := entries[rw.service]; !ok {
			return fmt.Errorf("%w: path=%s service=%s", ErrCredentialsNotExist, rw.path, rw.service)
		}

		delete(entries, rw.service)

		return nil
	})
}

// Location returns the plaintext credentials file's path.
func (rw FileReadWriter) Location() string {
	return rw.path
}

func (rw FileReadWriter) readMap() (map[string]string, error) {
	content, err := os.ReadFile(rw.path)
	if errors.Is(err, os.ErrNotExist) {
		return make(map[string]string), nil
	}

	if err != nil {
		return nil, fmt.Errorf("failed to read credentials file %s: %s", rw.path, err.Error())
	}

	var entries map[string]string
	if err = json.Unmarshal(content, &entries); err != nil {
		return nil, fmt.Errorf("failed to parse credentials file %s: %s", rw.path, err.Error())
	}

	if entries == nil {
		return nil, fmt.Errorf("failed to parse credentials file %s: expected a JSON object", rw.path)
	}

	return entries, nil
}

func (rw FileReadWriter) update(change func(map[string]string) error) error {
	path, err := filepath.Abs(rw.path)
	if err != nil {
		return fmt.Errorf("failed to resolve credentials file %s: %s", rw.path, err.Error())
	}

	dir := filepath.Dir(path)
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create credentials directory %s: %s", dir, err.Error())
	}

	// The lock directory already scopes this key to the credentials' parent.
	// Using only the filename makes symlinks and Windows junctions share the
	// same physical lock file without changing final-file replacement semantics.
	lock, err := lockfile.Acquire(dir, "credentials", filepath.Base(path))
	if err != nil {
		return fmt.Errorf("failed to lock credentials file %s: %s", rw.path, err.Error())
	}

	defer func() { _ = lock.Close() }()

	entries, err := rw.readMap()
	if err != nil {
		return err
	}

	if err = change(entries); err != nil {
		return err
	}

	if len(entries) == 0 {
		if err = os.Remove(rw.path); err != nil {
			return fmt.Errorf("failed to remove credentials file %s: %s", rw.path, err.Error())
		}

		return nil
	}

	return rw.writeMap(entries)
}

func (rw FileReadWriter) writeMap(entries map[string]string) error {
	content, err := json.Marshal(entries)
	if err != nil {
		return fmt.Errorf("failed to encode credentials file %s: %s", rw.path, err.Error())
	}

	file, err := os.CreateTemp(filepath.Dir(rw.path), ".credentials-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temporary credentials file: %s", err.Error())
	}

	defer func() { _ = os.Remove(file.Name()) }()

	if _, err = file.Write(content); err != nil {
		_ = file.Close()

		return fmt.Errorf("failed to write credentials file %s: %s", rw.path, err.Error())
	}

	if err = file.Close(); err != nil {
		return fmt.Errorf("failed to close credentials file %s: %s", rw.path, err.Error())
	}

	if err = os.Rename(file.Name(), rw.path); err != nil {
		return fmt.Errorf("failed to replace credentials file %s: %s", rw.path, err.Error())
	}

	return nil
}
