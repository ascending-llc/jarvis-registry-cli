package skills

import (
	"errors"
	"fmt"

	"github.com/ascending-llc/jarvis-registry-cli/internal/lockfile"
)

// acquireLock protects this sync root with a per-user OS lock. An active lock
// never expires; process exit releases it automatically. The stable lock file
// remains in registryDir/locks after release so contenders share the same inode.
func acquireLock(registryDir, syncRoot string) (release func(), err error) {
	lock, err := lockfile.Acquire(registryDir, "skills", syncRoot)
	if errors.Is(err, lockfile.ErrInUse) {
		return nil, fmt.Errorf("a skills sync run is already in progress for %s; wait for it to finish and try again", syncRoot)
	}

	if err != nil {
		return nil, fmt.Errorf("could not acquire skills sync lock for %s: %s", syncRoot, err.Error())
	}

	return func() { _ = lock.Close() }, nil
}
