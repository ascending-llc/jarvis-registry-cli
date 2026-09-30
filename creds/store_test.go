package creds

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"
)

func TestNewStore(t *testing.T) {
	dir := t.TempDir()
	s := NewStore("service", "user", dir, true)
	assert.Equal(t, filepath.Join(dir, CredentialsFileName), s.Location())
	assert.Equal(t, runtime.GOOS == "linux", s.fallback)
	assert.True(t, s.fileOnly)
	assert.Equal(t, NewReadWriter("service", "user"), s.keyring)
}

func TestStoreFallback(t *testing.T) {
	for _, fallback := range []bool{true, false} {
		name := "disabled"
		if fallback {
			name = "enabled"
		}

		t.Run(name, func(t *testing.T) {
			keyring.MockInitWithError(errors.New("backend\nunavailable"))

			s, warnings := newTestStore(t, fallback, false)

			_, err := s.Read()
			if !fallback {
				require.ErrorContains(t, err, "backend")
				assert.NotErrorIs(t, err, ErrCredentialsNotExist)
				require.ErrorIs(t, s.Write([]byte("one")), ErrCredentialWriteFailure)
				assert.NoFileExists(t, s.file.path)
				assert.Empty(t, warnings.String())

				return
			}

			require.ErrorIs(t, err, ErrCredentialsNotExist)
			require.NoError(t, s.Write([]byte("one")))
			assert.Contains(t, warnings.String(), "warning: OS keyring unavailable")
			assert.Contains(t, warnings.String(), "credentials stored in plaintext at "+s.file.path)
			assert.Equal(t, 1, strings.Count(warnings.String(), "\n"))
			firstWarning := warnings.String()

			require.NoError(t, s.Write([]byte("two")))
			assert.Equal(t, firstWarning, warnings.String())

			content, err := s.Read()
			require.NoError(t, err)
			assert.Equal(t, "two", string(content))
			assert.Equal(t, s.file.path, s.Location())
			require.NoError(t, s.Delete(), "unavailable keyring must not prevent file logout on Linux")
			assert.NoFileExists(t, s.file.path)
			require.ErrorIs(t, s.Delete(), ErrCredentialsNotExist)
		})
	}
}

func TestStoreKeyring(t *testing.T) {
	keyring.MockInit()

	s, warnings := newTestStore(t, true, false)
	_, err := s.Read()
	require.ErrorIs(t, err, ErrCredentialsNotExist)
	assert.Equal(t, "keyring", s.Location())
	require.NoError(t, s.Write([]byte("value")))
	content, err := s.Read()
	require.NoError(t, err)
	assert.Equal(t, "value", string(content))
	assert.NoFileExists(t, s.file.path)
	assert.Empty(t, warnings.String())
}

func TestStoreFilePrecedence(t *testing.T) {
	keyring.MockInit()

	s, warnings := newTestStore(t, false, false)
	require.NoError(t, s.keyring.Write([]byte("old")))
	require.NoError(t, s.file.Write([]byte("new")))
	content, err := s.Read()
	require.NoError(t, err)
	assert.Equal(t, "new", string(content))
	require.NoError(t, s.Write([]byte("refreshed")))
	content, err = s.file.Read()
	require.NoError(t, err)
	assert.Equal(t, "refreshed", string(content))
	content, err = s.keyring.Read()
	require.NoError(t, err)
	assert.Equal(t, "old", string(content))
	assert.Empty(t, warnings.String())
}

func TestStoreFileOnly(t *testing.T) {
	keyring.MockInit()

	s, warnings := newTestStore(t, false, true)
	require.NoError(t, s.keyring.Write([]byte("old")))
	_, err := s.Read()
	require.ErrorIs(t, err, ErrCredentialsNotExist, "file-only must ignore cached keyring entries")
	require.NoError(t, s.Write([]byte("new")))
	content, err := s.keyring.Read()
	require.NoError(t, err)
	assert.Equal(t, "old", string(content))
	keyring.MockInitWithError(errors.New("must not use keyring"))
	require.NoError(t, s.Write([]byte("refreshed")))
	content, err = s.Read()
	require.NoError(t, err)
	assert.Equal(t, "refreshed", string(content))
	assert.Empty(t, warnings.String())
}

func TestStoreDelete(t *testing.T) {
	for _, tc := range []struct {
		name     string
		file     bool
		keyring  bool
		fileOnly bool
	}{
		{name: "empty"},
		{name: "keyring", keyring: true},
		{name: "file", file: true},
		{name: "both", file: true, keyring: true},
		{name: "file-only clears both", file: true, keyring: true, fileOnly: true},
		{name: "file-only clears old keyring", keyring: true, fileOnly: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			keyring.MockInit()

			s, _ := newTestStore(t, false, tc.fileOnly)
			other := NewFileReadWriter(s.file.path, "other")
			require.NoError(t, other.Write([]byte("keep")))

			if tc.file {
				require.NoError(t, s.file.Write([]byte("file")))
			}

			if tc.keyring {
				require.NoError(t, s.keyring.Write([]byte("keyring")))
			}

			err := s.Delete()
			if tc.file || tc.keyring {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, ErrCredentialsNotExist)
			}

			_, err = s.file.Read()
			require.ErrorIs(t, err, ErrCredentialsNotExist)
			_, err = s.keyring.Read()
			require.ErrorIs(t, err, ErrCredentialsNotExist)
			content, err := other.Read()
			require.NoError(t, err)
			assert.Equal(t, "keep", string(content))
		})
	}
}

func TestStoreDeleteBackendFailure(t *testing.T) {
	for _, fileOnly := range []bool{false, true} {
		keyring.MockInitWithError(errors.New("backend unavailable"))

		s, _ := newTestStore(t, false, fileOnly)
		require.NoError(t, s.file.Write([]byte("value")))
		require.ErrorContains(t, s.Delete(), "failed to delete credentials from OS keyring")
		assert.NoFileExists(t, s.file.path, "file cleanup still runs on keyring failure")
	}
}

func TestStoreFileFailure(t *testing.T) {
	keyring.MockInit()

	s, warnings := newTestStore(t, true, false)
	require.NoError(t, s.keyring.Write([]byte("old")))
	require.NoError(t, os.WriteFile(s.file.path, []byte("corrupt"), 0o600))
	_, err := s.Read()
	require.ErrorContains(t, err, s.file.path)
	assert.NotErrorIs(t, err, ErrCredentialsNotExist)
	require.ErrorIs(t, s.Write([]byte("new")), ErrCredentialWriteFailure)
	require.ErrorContains(t, s.Delete(), s.file.path)
	_, err = s.keyring.Read()
	require.ErrorIs(t, err, ErrCredentialsNotExist, "keyring cleanup still runs on file failure")
	content, err := os.ReadFile(s.file.path)
	require.NoError(t, err)
	assert.Equal(t, "corrupt", string(content))
	assert.Empty(t, warnings.String())
}

func TestStoreFailedFallbackDoesNotWarn(t *testing.T) {
	keyring.MockInitWithError(errors.New("backend unavailable"))

	s, warnings := newTestStore(t, true, false)
	// A directory at the lock-file parent prevents persistence after the read miss.
	require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(s.file.path), "locks"), nil, 0o600))
	require.ErrorIs(t, s.Write([]byte("value")), ErrCredentialWriteFailure)
	assert.NoFileExists(t, s.file.path)
	assert.Empty(t, warnings.String(), "do not claim plaintext storage when persistence failed")
}

func newTestStore(t *testing.T, fallback, fileOnly bool) (Store, *bytes.Buffer) {
	t.Helper()

	var warnings bytes.Buffer

	s := NewStore("service", "user", t.TempDir(), fileOnly)
	s.fallback = fallback
	s.warnOut = &warnings

	return s, &warnings
}
