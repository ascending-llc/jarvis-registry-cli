package creds

import (
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

type (
	// KeyringReadWriter reads, writes, and deletes an opaque credentials blob in
	// the OS keyring, scoped to a service and user.
	KeyringReadWriter struct {
		service string
		user    string
	}
)

var (
	// ErrCredentialsNotExist indicates no credentials are stored in the
	// credential store for the given service.
	ErrCredentialsNotExist = errors.New("credentials do not exist")

	// ErrCredentialWriteFailure indicates a credential store rejected a write.
	ErrCredentialWriteFailure = errors.New("failed to write credentials")
)

// NewReadWriter returns a KeyringReadWriter scoped to service and user.
func NewReadWriter(service, user string) KeyringReadWriter {
	return KeyringReadWriter{service: service, user: user}
}

// Read returns the credentials stored in the OS keyring for rw's service
// and user. It returns an error wrapping ErrCredentialsNotExist if none
// are stored.
func (rw KeyringReadWriter) Read() ([]byte, error) {
	key, err := keyring.Get(rw.service, rw.user)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return nil, fmt.Errorf("%w: service=%s user=%s", ErrCredentialsNotExist, rw.service, rw.user)
		}

		return nil, fmt.Errorf("failed to read credentials from OS keyring: service=%s user=%s: %s", rw.service, rw.user, err.Error())
	}

	return []byte(key), nil
}

// Write stores content in the OS keyring for rw's service and user,
// overwriting any existing value. It returns an error wrapping
// ErrCredentialWriteFailure on failure.
func (rw KeyringReadWriter) Write(content []byte) error {
	if err := keyring.Set(rw.service, rw.user, string(content)); err != nil {
		return fmt.Errorf("%w: service=%s user=%s: %s", ErrCredentialWriteFailure, rw.service, rw.user, err.Error())
	}

	return nil
}

// Delete removes the credentials, returning ErrCredentialsNotExist when absent.
func (rw KeyringReadWriter) Delete() error {
	if err := keyring.Delete(rw.service, rw.user); err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return fmt.Errorf("%w: service=%s user=%s", ErrCredentialsNotExist, rw.service, rw.user)
		}

		return fmt.Errorf("failed to delete credentials from OS keyring: service=%s user=%s: %s", rw.service, rw.user, err.Error())
	}

	return nil
}

// Location identifies the OS keyring as this store's location.
func (rw KeyringReadWriter) Location() string {
	return "keyring"
}
