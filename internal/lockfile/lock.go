// Package lockfile coordinates CLI operations with per-user, per-target OS locks.
package lockfile

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gofrs/flock"
)

// ErrInUse means another operation holds the lock for this namespace and target.
var ErrInUse = errors.New("lock is already in use")

// Acquire takes a nonblocking OS lock under registryDir/locks. Callers supply a
// fixed namespace and a stable target key: a resolved absolute path when this
// directory holds locks for multiple locations, or a filename when it is scoped
// to the target's parent. Distinct namespaces, keys, and physical lock directories
// do not contend. Close the lock when finished; process exit also releases it.
// Keep the file after closing: unlinking it can let contenders lock different
// inodes for the same target. A file's age never indicates whether it is held.
func Acquire(registryDir, namespace, target string) (*flock.Flock, error) {
	locksDir := filepath.Join(registryDir, "locks")
	if err := os.MkdirAll(locksDir, 0o700); err != nil {
		return nil, fmt.Errorf("could not create locks directory %q: %s", locksDir, err.Error())
	}

	sum := sha256.Sum256([]byte(filepath.Clean(target)))
	path := filepath.Join(locksDir, fmt.Sprintf("%s-%x.lock", namespace, sum))
	lock := flock.New(path, flock.SetPermissions(0o600))
	locked, err := lock.TryLock()
	if err != nil {
		_ = lock.Close()

		return nil, fmt.Errorf("could not acquire lock %q: %s", path, err.Error())
	}

	if !locked {
		_ = lock.Close()

		return nil, fmt.Errorf("%w: %s", ErrInUse, path)
	}

	return lock, nil
}
