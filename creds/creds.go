package creds

import (
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

type (
	KeyringReadWriter struct {
		service string
		user    string
	}
)

var (
	ErrCredentialsNotExist    = errors.New("credentials do not exist")
	ErrCredentialWriteFailure = errors.New("failed to write credentials to OS keyring")
)

func NewReadWriter(service, user string) KeyringReadWriter {
	return KeyringReadWriter{service: service, user: user}
}

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

func (rw KeyringReadWriter) Write(content []byte) error {
	if err := keyring.Set(rw.service, rw.user, string(content)); err != nil {
		return fmt.Errorf("%w: service=%s user=%s: %s", ErrCredentialWriteFailure, rw.service, rw.user, err.Error())
	}

	return nil
}
