package update

import (
	"fmt"
	"path/filepath"

	"github.com/gofrs/flock"
)

// acquireUpdateLock protects the resolved executable's staging and backup files.
// Lock a separate, stable file because UpdateTo replaces the executable's inode.
// Keep this file after unlocking: deleting it could let another process lock a
// new inode while an existing contender still holds the old one. The OS releases
// the lock when its owning process exits, including abnormal termination.
func acquireUpdateLock(exe string) (*flock.Flock, error) {
	path := filepath.Join(filepath.Dir(exe), "."+filepath.Base(exe)+".update.lock")
	lock := flock.New(path)
	locked, err := lock.TryLock()
	if err != nil {
		_ = lock.Close()

		return nil, fmt.Errorf("could not acquire update lock for %q (check directory permissions): %s", exe, err.Error())
	}

	if !locked {
		_ = lock.Close()

		return nil, fmt.Errorf("another update is already in progress for %q; wait for it to finish and try again", exe)
	}

	return lock, nil
}
