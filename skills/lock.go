package skills

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// lockStaleAfter is how long an advisory lock file may sit unclaimed
// before a subsequent acquireLock call is allowed to reclaim it, on the
// assumption that whatever process created it crashed or was killed
// without releasing it.
const lockStaleAfter = 30 * time.Second

// acquireLock takes an advisory, per-pluginRoot lock so two concurrent
// sync-skills invocations against the same target can't race on the
// consent check or the bootstrap writes. The lock file lives under
// <registryDir>/locks/, named by the SHA-256 hex digest of the cleaned,
// absolute pluginRoot path, so distinct sync targets never contend and
// the same target always maps to the same lock file. It is created with
// O_EXCL, a hand-rolled, stdlib-only PID-file lock: a second invocation
// against the same target either reclaims a stale lock or fails fast
// with an actionable message. On success, release must be called to
// remove the lock file once the caller is done.
func acquireLock(registryDir, pluginRoot string) (release func(), err error) {
	locksDir := filepath.Join(registryDir, "locks")
	if err = os.MkdirAll(locksDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create locks directory at %s: %s", locksDir, err.Error())
	}

	sum := sha256.Sum256([]byte(filepath.Clean(pluginRoot)))
	lockPath := filepath.Join(locksDir, hex.EncodeToString(sum[:])+".lock")

	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		if !errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("failed to create lock file at %s: %s", lockPath, err.Error())
		}

		if !reclaimStaleLock(lockPath) {
			return nil, fmt.Errorf("a sync-skills run is already in progress for %s (lock file %s); wait for it to finish, or remove the lock file if you're sure it's stale", pluginRoot, lockPath)
		}

		if f, err = os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644); err != nil {
			return nil, fmt.Errorf("failed to create lock file at %s after reclaiming a stale one: %s", lockPath, err.Error())
		}
	}

	_, _ = fmt.Fprintf(f, "pid=%d\nstarted=%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339))
	_ = f.Close()

	return func() { _ = os.Remove(lockPath) }, nil
}

// reclaimStaleLock removes lockPath if its mtime is older than
// lockStaleAfter.
func reclaimStaleLock(lockPath string) bool {
	stat, err := os.Stat(lockPath)
	if err != nil || time.Since(stat.ModTime()) < lockStaleAfter {
		return false
	}

	return os.Remove(lockPath) == nil
}
