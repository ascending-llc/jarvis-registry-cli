package lockfile

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAcquire(t *testing.T) {
	registryDir := filepath.Join(t.TempDir(), ".jarvis-registry")
	target := filepath.Join(t.TempDir(), "jarvis-registry")
	lock, err := Acquire(registryDir, "update", target)
	require.NoError(t, err)
	t.Cleanup(func() { _ = lock.Close() })

	sum := sha256.Sum256([]byte(filepath.Clean(target)))
	expected := filepath.Join(registryDir, "locks", fmt.Sprintf("update-%x.lock", sum))
	assert.Equal(t, expected, lock.Path())
	require.FileExists(t, expected)

	alias := filepath.Dir(target) + string(filepath.Separator) + "." + string(filepath.Separator) + filepath.Base(target)
	contender, err := Acquire(registryDir, "update", alias)
	require.ErrorIs(t, err, ErrInUse, "cleaned paths to the same target must contend")
	assert.Nil(t, contender)

	for _, tc := range []struct {
		name        string
		registryDir string
		namespace   string
		target      string
	}{
		{name: "different namespace", registryDir: registryDir, namespace: "skills", target: target},
		{name: "different target", registryDir: registryDir, namespace: "update", target: target + "-other"},
		{name: "different user state", registryDir: t.TempDir(), namespace: "update", target: target},
	} {
		t.Run(tc.name, func(t *testing.T) {
			other, acquireErr := Acquire(tc.registryDir, tc.namespace, tc.target)
			require.NoError(t, acquireErr)
			require.NoError(t, other.Close())
		})
	}

	require.NoError(t, lock.Close())
	require.FileExists(t, expected, "release must preserve the stable lock file")

	retry, err := Acquire(registryDir, "update", target)
	require.NoError(t, err, "a leftover file is not a held lock")
	require.NoError(t, retry.Close())
}

func TestAcquireNeverExpiresHeldLock(t *testing.T) {
	registryDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "skills")
	lock, err := Acquire(registryDir, "skills", target)
	require.NoError(t, err)
	t.Cleanup(func() { _ = lock.Close() })

	old := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(lock.Path(), old, old))

	contender, err := Acquire(registryDir, "skills", target)
	require.ErrorIs(t, err, ErrInUse, "mtime must not let a second operation steal an active lock")
	assert.Nil(t, contender)

	require.NoError(t, lock.Close())

	retry, err := Acquire(registryDir, "skills", target)
	require.NoError(t, err)
	require.NoError(t, retry.Close())
}

func TestAcquireDirectoryFailure(t *testing.T) {
	registryDir := filepath.Join(t.TempDir(), "state-file")
	require.NoError(t, os.WriteFile(registryDir, nil, 0o600))

	lock, err := Acquire(registryDir, "update", filepath.Join(t.TempDir(), "jarvis-registry"))
	require.ErrorContains(t, err, "could not create locks directory")
	assert.NotErrorIs(t, err, ErrInUse)
	assert.Nil(t, lock)
}
