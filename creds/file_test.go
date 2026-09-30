package creds

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ascending-llc/jarvis-registry-cli/internal/lockfile"
)

func TestFileReadWriterLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new", CredentialsFileName)
	first := NewFileReadWriter(path, "first")
	second := NewFileReadWriter(path, "second")
	assert.Equal(t, path, first.Location())
	_, err := first.Read()
	require.ErrorIs(t, err, ErrCredentialsNotExist)
	require.ErrorIs(t, first.Delete(), ErrCredentialsNotExist)
	assert.NoDirExists(t, filepath.Dir(path), "deleting absent credentials must not create storage")
	require.NoError(t, first.Write([]byte("first value")))

	_, err = second.Read()
	require.ErrorIs(t, err, ErrCredentialsNotExist)
	require.ErrorIs(t, second.Delete(), ErrCredentialsNotExist)
	require.NoError(t, second.Write([]byte("second value")))
	require.NoError(t, first.Write([]byte("updated value")))
	got, err := first.Read()
	require.NoError(t, err)
	assert.Equal(t, "updated value", string(got))
	require.NoError(t, first.Delete())

	got, err = second.Read()
	require.NoError(t, err)
	assert.Equal(t, "second value", string(got))
	require.NoError(t, second.Delete())
	assert.NoFileExists(t, path)
}

func TestFileReadWriterPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits")
	}

	for _, existing := range []bool{false, true} {
		name := "new"
		if existing {
			name = "existing"
		}

		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "parent", CredentialsFileName)
			if existing {
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
				require.NoError(t, os.WriteFile(path, []byte(`{}`), 0o644))
				require.NoError(t, os.Chmod(path, 0o644))
			}

			rw := NewFileReadWriter(path, "service")
			require.NoError(t, rw.Write([]byte("value")))

			info, err := os.Stat(path)
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
			info, err = os.Stat(filepath.Dir(path))
			require.NoError(t, err)

			expected := os.FileMode(0o700)
			if existing {
				expected = 0o755
			}

			assert.Equal(t, expected, info.Mode().Perm())
		})
	}
}

func TestFileReadWriterRejectsCorruptFile(t *testing.T) {
	for _, content := range []string{"broken", "", "null", "[]", `{"service": 42}`} {
		t.Run(content, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), CredentialsFileName)
			require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
			rw := NewFileReadWriter(path, "service")
			_, err := rw.Read()
			require.ErrorContains(t, err, path)
			assert.NotErrorIs(t, err, ErrCredentialsNotExist)
			err = rw.Write([]byte("replacement"))
			require.ErrorIs(t, err, ErrCredentialWriteFailure)
			require.ErrorContains(t, rw.Delete(), path)
			got, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, content, string(got), "invalid existing data must survive")
		})
	}
}

func TestFileReadWriterIOFailure(t *testing.T) {
	dir := t.TempDir()
	rw := NewFileReadWriter(dir, "service")
	_, err := rw.Read()
	require.ErrorContains(t, err, dir)
	assert.NotErrorIs(t, err, ErrCredentialsNotExist)

	parent := filepath.Join(dir, "regular-file")
	require.NoError(t, os.WriteFile(parent, []byte("unchanged"), 0o600))
	rw = NewFileReadWriter(filepath.Join(parent, CredentialsFileName), "service")
	require.ErrorIs(t, rw.Write([]byte("value")), ErrCredentialWriteFailure)
	require.Error(t, rw.Delete())

	content, err := os.ReadFile(parent)
	require.NoError(t, err)
	assert.Equal(t, "unchanged", string(content))
}

func TestFileReadWriterLockContention(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, CredentialsFileName)
	first := NewFileReadWriter(path, "first")
	second := NewFileReadWriter(path, "second")

	require.NoError(t, first.Write([]byte("original")))

	lock, err := lockfile.Acquire(dir, "credentials", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = lock.Close() })

	err = second.Write([]byte("second"))
	require.ErrorIs(t, err, ErrCredentialWriteFailure)
	assert.Contains(t, err.Error(), "lock is already in use")
	require.ErrorContains(t, first.Delete(), "lock is already in use")
	content, err := first.Read()
	require.NoError(t, err)
	assert.Equal(t, "original", string(content))
	require.NoError(t, lock.Close())
	require.NoError(t, second.Write([]byte("second")))

	content, err = first.Read()
	require.NoError(t, err)
	assert.Equal(t, "original", string(content))
}
