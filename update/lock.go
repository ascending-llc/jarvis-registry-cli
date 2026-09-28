package update

import (
	"errors"
	"fmt"

	"github.com/gofrs/flock"

	"github.com/ascending-llc/jarvis-registry-cli/internal/lockfile"
)

// acquireUpdateLock protects the resolved executable's staging and backup files
// using the shared per-user lock directory, without writing beside the binary.
func acquireUpdateLock(registryDir, exe string) (*flock.Flock, error) {
	lock, err := lockfile.Acquire(registryDir, "update", exe)
	if errors.Is(err, lockfile.ErrInUse) {
		return nil, fmt.Errorf("another update is already in progress for %q; wait for it to finish and try again", exe)
	}

	if err != nil {
		return nil, fmt.Errorf("could not acquire update lock for %q: %s", exe, err.Error())
	}

	return lock, nil
}
