package creds

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"
)

func TestKeyringReadWriter_WriteThenRead(t *testing.T) {
	keyring.MockInit()

	rw := NewReadWriter("test-service", "test-user")

	err := rw.Write([]byte("secret-content"))
	require.NoError(t, err, "Write should succeed against the mocked keyring")

	content, err := rw.Read()
	require.NoError(t, err, "Read should succeed after a prior Write")

	assert.Equal(t, "secret-content", string(content), "Read should return exactly what was previously Written")
}

func TestKeyringReadWriter_ReadNotExist(t *testing.T) {
	keyring.MockInit()

	rw := NewReadWriter("test-service", "test-user")

	_, err := rw.Read()
	require.Error(t, err, "Read should fail when nothing has been written for this service/user")

	assert.ErrorIs(t, err, ErrCredentialsNotExist, "Read should wrap ErrCredentialsNotExist when the keyring has no matching entry")
}

func TestKeyringReadWriter_ReadOtherFailure(t *testing.T) {
	wantErr := errors.New("keyring backend unavailable")
	keyring.MockInitWithError(wantErr)

	rw := NewReadWriter("test-service", "test-user")

	_, err := rw.Read()
	require.Error(t, err, "Read should surface a non-ErrNotFound keyring failure")

	assert.NotErrorIs(t, err, ErrCredentialsNotExist, "a generic keyring failure should not be reported as ErrCredentialsNotExist")
	assert.Contains(t, err.Error(), wantErr.Error(), "the error message should include the underlying keyring failure")
}

func TestKeyringReadWriter_WriteFailure(t *testing.T) {
	wantErr := errors.New("keyring backend unavailable")
	keyring.MockInitWithError(wantErr)

	rw := NewReadWriter("test-service", "test-user")

	err := rw.Write([]byte("secret-content"))
	require.Error(t, err, "Write should surface a keyring failure")

	assert.ErrorIs(t, err, ErrCredentialWriteFailure, "Write should wrap ErrCredentialWriteFailure on a keyring failure")
}
