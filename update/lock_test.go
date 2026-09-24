package update

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const processLockTargetEnv = "JARVIS_UPDATE_LOCK_TEST_TARGET"

func TestAcquireUpdateLock(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "jarvis-registry")
	lock, err := acquireUpdateLock(exe)
	require.NoError(t, err)
	t.Cleanup(func() { _ = lock.Close() })

	contender, err := acquireUpdateLock(exe)
	require.ErrorContains(t, err, "another update is already in progress")
	assert.Nil(t, contender)

	other, err := acquireUpdateLock(filepath.Join(t.TempDir(), "jarvis-registry"))
	require.NoError(t, err, "distinct installations must not contend")
	require.NoError(t, other.Close())
	require.NoError(t, lock.Close())
	require.FileExists(t, lock.Path(), "keep the lock inode stable across updates")

	retry, err := acquireUpdateLock(exe)
	require.NoError(t, err, "a leftover lock file does not indicate an active owner")
	require.NoError(t, retry.Close())
}

func TestCommandLockFailureDoesNotInstall(t *testing.T) {
	_, _, release := newReleaseFixture(t)
	updater := &updaterStub{release: release, found: true}
	cmd := testCommand(t, updater, &bytes.Buffer{})
	exe := filepath.Join(t.TempDir(), "missing-directory", "jarvis-registry")
	cmd.executablePathFunc = func() (string, error) { return exe, nil }

	err := cmd.Run()

	require.ErrorContains(t, err, "could not acquire update lock")
	assert.Zero(t, updater.updateCalls)
}

func TestCommandReleasesLockAfterFailure(t *testing.T) {
	_, _, release := newReleaseFixture(t)
	updater := &updaterStub{release: release, found: true, updateErr: errors.New("installation failed")}
	cmd := testCommand(t, updater, &bytes.Buffer{})
	require.ErrorContains(t, cmd.Run(), "installation failed")

	updater.updateErr = nil

	require.NoError(t, cmd.Run(), "a failed installation must release its lock")
	assert.Equal(t, 2, updater.updateCalls)
}

// TestCommandConcurrentUpdates exercises independent updater instances against
// one installation, including the real archive validation and file replacement.
func TestCommandConcurrentUpdates(t *testing.T) {
	for range 32 {
		exe := filepath.Join(t.TempDir(), testBinaryName())
		require.NoError(t, os.WriteFile(exe, readFixture(t, "old-binary.txt"), 0o600))

		commands := make([]Command, 4)
		for i := range commands {
			commands[i] = newInstallCommand(t, exe)
		}

		var workers sync.WaitGroup

		start := make(chan struct{})

		results := make(chan error, len(commands))
		for i := range commands {
			workers.Add(1)
			go func(index int) {
				defer workers.Done()

				<-start

				results <- commands[index].Run()
			}(i)
		}

		close(start)
		workers.Wait()
		close(results)

		successes := 0
		for err := range results {
			if err == nil {
				successes++

				continue
			}

			require.ErrorContains(t, err, "another update is already in progress")
		}

		require.Positive(t, successes)

		actual, err := os.ReadFile(exe)
		require.NoError(t, err, "concurrent updates must never remove the executable")
		assert.Equal(t, readFixture(t, "new-binary.txt"), actual)
	}
}

// TestUpdateLockAcrossProcesses verifies that another process blocks installation
// but not --check, and that both normal exit and termination release the lock.
func TestUpdateLockAcrossProcesses(t *testing.T) {
	for _, terminate := range []bool{false, true} {
		t.Run(fmt.Sprintf("terminate=%t", terminate), func(t *testing.T) {
			exe := filepath.Join(t.TempDir(), testBinaryName())
			oldBytes := readFixture(t, "old-binary.txt")
			require.NoError(t, os.WriteFile(exe, oldBytes, 0o600))
			child, stdin := startLockHolder(t, exe)
			cmd := newInstallCommand(t, exe)

			err := cmd.Run()
			require.ErrorContains(t, err, "another update is already in progress")
			actual, err := os.ReadFile(exe)
			require.NoError(t, err)
			assert.Equal(t, oldBytes, actual)

			cmd.Check = true
			require.NoError(t, cmd.Run(), "--check must work while another process owns the lock")

			if terminate {
				require.NoError(t, child.Process.Kill())
				require.Error(t, child.Wait())
			} else {
				require.NoError(t, stdin.Close())
				require.NoError(t, child.Wait())
			}

			cmd.Check = false
			require.NoError(t, cmd.Run(), "update must work after the owner exits without deleting the lock file")

			actual, err = os.ReadFile(exe)
			require.NoError(t, err)
			assert.Equal(t, readFixture(t, "new-binary.txt"), actual)
		})
	}
}

// TestUpdateLockProcessHelper is only selected by startLockHolder's subprocess.
func TestUpdateLockProcessHelper(t *testing.T) {
	target := os.Getenv(processLockTargetEnv)
	if target == "" {
		t.Skip("subprocess helper")
	}

	lock, err := acquireUpdateLock(target)
	require.NoError(t, err)
	_, err = io.WriteString(os.Stdout, "update-lock-ready\n")
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, os.Stdin)
	require.NoError(t, err)
	require.NoError(t, lock.Close())
}

func startLockHolder(t *testing.T, target string) (*exec.Cmd, io.WriteCloser) {
	t.Helper()

	executable, err := os.Executable()
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	t.Cleanup(cancel)

	child := exec.CommandContext(ctx, executable, "-test.run=^TestUpdateLockProcessHelper$")

	child.Env = append(os.Environ(), processLockTargetEnv+"="+target)
	stdin, err := child.StdinPipe()
	require.NoError(t, err)
	stdout, err := child.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, child.Start())
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = child.Process.Kill()
		_ = child.Wait()
	})

	ready, err := bufio.NewReader(stdout).ReadString('\n')
	require.NoError(t, err, "lock holder exited before reporting ready")
	require.Equal(t, "update-lock-ready", strings.TrimSpace(ready))

	return child, stdin
}

func newInstallCommand(t *testing.T, exe string) Command {
	t.Helper()
	source, updater, release := newReleaseFixture(t)
	archive := makeArchive(t, release.AssetName, filepath.Base(exe), readFixture(t, "new-binary.txt"))
	source.assets[release.AssetID] = archive
	source.assets[release.ValidationAssetID] = fmt.Appendf(nil, "%x  %s\n", sha256.Sum256(archive), release.AssetName)
	cmd := testCommand(t, updater, &bytes.Buffer{})
	cmd.executablePathFunc = func() (string, error) { return exe, nil }

	return cmd
}

func testBinaryName() string {
	if runtime.GOOS == "windows" {
		return "jarvis-registry.exe"
	}

	return "jarvis-registry"
}
