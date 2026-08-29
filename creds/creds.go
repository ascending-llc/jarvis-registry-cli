package creds

import (
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

type (
	// KeyringReadWriter reads and writes an opaque credentials blob to
	// the OS keyring, scoped to a service and user.
	KeyringReadWriter struct {
		service string
		user    string
	}
)

var (
	// ErrCredentialsNotExist indicates no credentials are stored in the
	// OS keyring for the given service and user.
	ErrCredentialsNotExist = errors.New("credentials do not exist")

	// ErrCredentialWriteFailure indicates the OS keyring rejected a
	// write.
	ErrCredentialWriteFailure = errors.New("failed to write credentials to OS keyring")
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

		return nil, fmt.Errorf("credentials exist but cannot be read: service=%s user=%s: %s", rw.service, rw.user, err.Error())
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
