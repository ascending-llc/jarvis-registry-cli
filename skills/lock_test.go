package skills

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAcquireLock(t *testing.T) {
	t.Run("acquires and releases a lock for a fresh target", func(t *testing.T) {
		registryDir := t.TempDir()
		pluginRoot := filepath.Join(t.TempDir(), "plugin-root")

		release, err := acquireLock(registryDir, pluginRoot)
		require.NoError(t, err, "acquireLock should succeed for a target with no existing lock")

		entries, err := os.ReadDir(filepath.Join(registryDir, "locks"))
		require.NoError(t, err, "should be able to list the locks directory")
		assert.Len(t, entries, 1, "acquireLock should create exactly one lock file")

		release()

		entries, err = os.ReadDir(filepath.Join(registryDir, "locks"))
		require.NoError(t, err, "should be able to list the locks directory after release")
		assert.Len(t, entries, 1, "release must keep the stable lock file")

		retry, err := acquireLock(registryDir, pluginRoot)
		require.NoError(t, err, "a released lock file must not block the next sync")
		retry()
	})

	t.Run("a second acquireLock against the same target fails while the first is held", func(t *testing.T) {
		registryDir := t.TempDir()
		pluginRoot := filepath.Join(t.TempDir(), "plugin-root")

		release, err := acquireLock(registryDir, pluginRoot)
		require.NoError(t, err, "the first acquireLock should succeed")

		t.Cleanup(release)

		_, err = acquireLock(registryDir, pluginRoot)
		require.Error(t, err, "a second acquireLock against the same target should fail while the first lock is held")
		assert.Contains(t, err.Error(), "already in progress", "the error should explain that a sync is already in progress")
	})

	t.Run("two different targets never contend", func(t *testing.T) {
		registryDir := t.TempDir()
		pluginRootA := filepath.Join(t.TempDir(), "plugin-root-a")
		pluginRootB := filepath.Join(t.TempDir(), "plugin-root-b")

		releaseA, err := acquireLock(registryDir, pluginRootA)
		require.NoError(t, err, "acquiring the lock for target A should succeed")

		t.Cleanup(releaseA)

		releaseB, err := acquireLock(registryDir, pluginRootB)
		require.NoError(t, err, "acquiring the lock for target B should succeed while target A's lock is still held")

		t.Cleanup(releaseB)

		entries, err := os.ReadDir(filepath.Join(registryDir, "locks"))
		require.NoError(t, err, "should be able to list the locks directory")
		assert.Len(t, entries, 2, "two distinct targets should produce two distinct lock files")
	})

	t.Run("an old lock file is not reclaimed while its owner is still running", func(t *testing.T) {
		registryDir := t.TempDir()
		pluginRoot := filepath.Join(t.TempDir(), "plugin-root")
		release, err := acquireLock(registryDir, pluginRoot)
		require.NoError(t, err)
		t.Cleanup(release)

		entries, err := os.ReadDir(filepath.Join(registryDir, "locks"))
		require.NoError(t, err)
		require.Len(t, entries, 1)

		old := time.Now().Add(-time.Hour)
		require.NoError(t, os.Chtimes(filepath.Join(registryDir, "locks", entries[0].Name()), old, old))

		release2, err := acquireLock(registryDir, pluginRoot)
		require.ErrorContains(t, err, "already in progress")
		assert.Nil(t, release2)
		assert.NotContains(t, err.Error(), "remove the lock file")

		release()

		release2, err = acquireLock(registryDir, pluginRoot)
		require.NoError(t, err, "only releasing the OS lock allows the next sync")
		release2()
	})
}

func TestAcquireLockDirectoryFailure(t *testing.T) {
	registryDir := filepath.Join(t.TempDir(), "state-file")
	require.NoError(t, os.WriteFile(registryDir, nil, 0o600))

	release, err := acquireLock(registryDir, filepath.Join(t.TempDir(), "skills"))
	require.ErrorContains(t, err, "could not acquire skills sync lock")
	assert.Nil(t, release)
}
