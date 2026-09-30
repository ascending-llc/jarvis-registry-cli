package creds

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Store selects the OS keyring or a plaintext file for cached credentials.
// File entries remain authoritative; automatic fallback is Linux-only.
type Store struct {
	warnOut  io.Writer
	keyring  KeyringReadWriter
	file     FileReadWriter
	fileOnly bool
	fallback bool
}

// CredentialsFileName is the plaintext credentials file under the Registry directory.
const CredentialsFileName = "credentials.json"

// NewStore returns the CLI's credential store for service and user.
// fileOnly bypasses the keyring for reads and writes on every OS.
func NewStore(service, user, registryDir string, fileOnly bool) Store {
	return Store{
		keyring:  NewReadWriter(service, user),
		file:     NewFileReadWriter(filepath.Join(registryDir, CredentialsFileName), service),
		warnOut:  os.Stderr,
		fileOnly: fileOnly,
		fallback: runtime.GOOS == "linux",
	}
}

// Read prefers an existing file entry, then tries the keyring unless fileOnly.
// On Linux an unavailable keyring with no file entry is treated as a cache miss.
func (s Store) Read() ([]byte, error) {
	content, err := s.file.Read()
	if s.fileOnly || !errors.Is(err, ErrCredentialsNotExist) {
		return content, err
	}

	content, err = s.keyring.Read()
	if err != nil && s.fallback {
		return nil, ErrCredentialsNotExist
	}

	return content, err
}

// Write updates an existing file entry or tries the keyring, falling back on Linux.
// Automatic fallback warns only after creating a file entry successfully.
func (s Store) Write(content []byte) error {
	if s.fileOnly {
		return s.file.Write(content)
	}

	_, err := s.file.Read()
	if err == nil {
		return s.file.Write(content)
	}

	if !errors.Is(err, ErrCredentialsNotExist) {
		return fmt.Errorf("%w: %s", ErrCredentialWriteFailure, err.Error())
	}

	keyringErr := s.keyring.Write(content)
	if keyringErr == nil || !s.fallback {
		return keyringErr
	}

	if err = s.file.Write(content); err != nil {
		return err
	}
	// Keep a backend's multiline diagnostics from turning the warning into several lines.
	reason := strings.Join(strings.Fields(keyringErr.Error()), " ")
	_, _ = fmt.Fprintf(s.warnOut, "warning: OS keyring unavailable (%s); credentials stored in plaintext at %s\n", reason, s.file.Location())

	return nil
}

// Delete attempts both stores even if one fails, including in file-only mode.
// Only Linux automatic fallback ignores unavailable keyrings; file-only mode reports errors.
func (s Store) Delete() error {
	fileErr := s.file.Delete()
	keyringErr := s.keyring.Delete()

	var failures []error
	if fileErr != nil && !errors.Is(fileErr, ErrCredentialsNotExist) {
		failures = append(failures, fileErr)
	}

	if keyringErr != nil && !errors.Is(keyringErr, ErrCredentialsNotExist) && (!s.fallback || s.fileOnly) {
		failures = append(failures, keyringErr)
	}

	if len(failures) > 0 {
		return errors.Join(failures...)
	}

	if fileErr == nil || keyringErr == nil {
		return nil
	}

	return ErrCredentialsNotExist
}

// Location reports the active file path or "keyring".
func (s Store) Location() string {
	if s.fileOnly {
		return s.file.Location()
	}

	if _, err := s.file.Read(); err == nil {
		return s.file.Location()
	}

	return s.keyring.Location()
}
