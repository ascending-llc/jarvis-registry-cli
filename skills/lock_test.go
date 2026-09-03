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
		assert.Empty(t, entries, "release should remove the lock file")
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

	t.Run("a stale lock is reclaimed rather than blocking a new acquisition", func(t *testing.T) {
		registryDir := t.TempDir()
		pluginRoot := filepath.Join(t.TempDir(), "plugin-root")

		release, err := acquireLock(registryDir, pluginRoot)
		require.NoError(t, err, "the first acquireLock should succeed")

		entries, err := os.ReadDir(filepath.Join(registryDir, "locks"))
		require.NoError(t, err, "should be able to list the locks directory")
		require.Len(t, entries, 1, "the first acquireLock should create exactly one lock file")

		staleLockPath := filepath.Join(registryDir, "locks", entries[0].Name())

		staleTime := time.Now().Add(-2 * lockStaleAfter)
		require.NoError(t, os.Chtimes(staleLockPath, staleTime, staleTime), "should be able to backdate the lock file's mtime to simulate staleness")

		release2, err := acquireLock(registryDir, pluginRoot)
		require.NoError(t, err, "acquireLock should reclaim a stale lock rather than fail")

		t.Cleanup(release2)

		// release is now a no-op against a lock file that reclamation
		// already removed and acquireLock already recreated; nothing
		// further to assert here beyond acquireLock's own success above.
		release()
	})
}

func TestReclaimStaleLock(t *testing.T) {
	t.Run("does not reclaim a fresh lock file", func(t *testing.T) {
		lockPath := filepath.Join(t.TempDir(), "test.lock")

		require.NoError(t, os.WriteFile(lockPath, []byte("pid=1\n"), 0644), "should be able to write a fresh lock file")

		assert.False(t, reclaimStaleLock(lockPath), "a fresh lock file should not be reclaimed")

		_, err := os.Stat(lockPath)
		assert.NoError(t, err, "a fresh lock file should not be removed")
	})

	t.Run("reclaims a lock file older than lockStaleAfter", func(t *testing.T) {
		lockPath := filepath.Join(t.TempDir(), "test.lock")

		require.NoError(t, os.WriteFile(lockPath, []byte("pid=1\n"), 0644), "should be able to write a lock file")

		staleTime := time.Now().Add(-2 * lockStaleAfter)
		require.NoError(t, os.Chtimes(lockPath, staleTime, staleTime), "should be able to backdate the lock file's mtime")

		assert.True(t, reclaimStaleLock(lockPath), "a lock file older than lockStaleAfter should be reclaimed")

		_, err := os.Stat(lockPath)
		assert.True(t, os.IsNotExist(err), "a reclaimed lock file should have been removed")
	})

	t.Run("reports false for a lock file that does not exist", func(t *testing.T) {
		assert.False(t, reclaimStaleLock(filepath.Join(t.TempDir(), "missing.lock")), "a missing lock file should not be reported as reclaimed")
	})
}
