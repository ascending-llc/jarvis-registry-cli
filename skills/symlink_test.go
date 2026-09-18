package skills

import (
	"bytes"
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	junction "github.com/nyaosorg/go-windows-junction"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ascending-llc/jarvis-registry-cli/cfg"
)

func newLinkTestCommand(t *testing.T) (*SyncCommand, string, *bytes.Buffer) {
	t.Helper()

	home := t.TempDir()
	output := &bytes.Buffer{}
	cmd := &SyncCommand{
		userHomeDir:  home,
		mode:         cfg.SkillsModeCodex,
		destDir:      filepath.Join(home, ".jarvis-registry", "skills", "codex"),
		stderrLogger: log.New(output, "", 0),
		stdin:        strings.NewReader(""),
	}
	entry := filepath.Join(home, ".codex", "skills")

	require.NoError(t, os.MkdirAll(cmd.destDir, 0700), "should be able to create the CLI-owned destDir")
	require.NoError(t, os.MkdirAll(entry, 0700), "should be able to create the tool skills directory")

	return cmd, entry, output
}

func TestSymlinkScopePaths(t *testing.T) {
	home := t.TempDir()

	for _, tc := range []struct {
		mode cfg.SkillsMode
		dir  string
	}{
		{mode: cfg.SkillsModeCodex, dir: ".codex"},
		{mode: cfg.SkillsModeCopilot, dir: ".copilot"},
	} {
		dir, err := userScopeSkillsDir(home, tc.mode)
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(home, tc.dir, "skills"), dir)
		assert.Equal(t, filepath.Join(home, ".jarvis-registry", "skills", string(tc.mode)), personalScopeSyncRoot(filepath.Join(home, ".jarvis-registry"), tc.mode))
	}

	_, err := userScopeSkillsDir(home, cfg.SkillsModeClaude)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "claude")
}

func TestSymlinkCreatesAndPreservesCorrectLink(t *testing.T) {
	cmd, entry, _ := newLinkTestCommand(t)
	target := filepath.Join(cmd.destDir, "hello")
	require.NoError(t, os.Mkdir(target, 0700))

	for _, want := range []string{linkStatusLinked, linkStatusUnchanged} {
		outcomes, err := cmd.reconcileSymlinks([]string{"hello"})
		require.NoError(t, err)
		require.Len(t, outcomes, 1)
		assert.Equal(t, want, outcomes[0].Status)
		require.NoError(t, outcomes[0].Err)

		_, err = os.Readlink(filepath.Join(entry, "hello"))
		require.NoError(t, err, "entry must be readable as a link on every platform")

		gotInfo, err := os.Stat(filepath.Join(entry, "hello"))
		require.NoError(t, err)
		wantInfo, err := os.Stat(target)
		require.NoError(t, err)
		assert.True(t, os.SameFile(gotInfo, wantInfo))
	}
}

func TestSymlinkCollisionPolicy(t *testing.T) {
	policies := []struct {
		name        string
		answer      string
		override    bool
		interactive bool
		replace     bool
	}{
		{name: "default protects existing entry"},
		{name: "override replaces", override: true, replace: true},
		{name: "interactive yes", interactive: true, answer: "yes\n", replace: true},
		{name: "interactive YES is accepted", interactive: true, answer: "YES\n", replace: true},
		{name: "interactive y is accepted", interactive: true, answer: "y\n", replace: true},
		{name: "interactive no beats override", override: true, interactive: true, answer: "n\n"},
		{name: "interactive empty declines", interactive: true, answer: "\n"},
		{name: "interactive EOF declines", override: true, interactive: true},
	}

	for _, kind := range []string{"file", "directory", "link", "dangling link"} {
		for _, policy := range policies {
			t.Run(kind+"/"+policy.name, func(t *testing.T) {
				cmd, entry, output := newLinkTestCommand(t)
				cmd.override, cmd.Interactive = policy.override, policy.interactive
				cmd.stdin = strings.NewReader(policy.answer)
				link := filepath.Join(entry, "hello")
				target := filepath.Join(cmd.destDir, "hello")
				require.NoError(t, os.Mkdir(target, 0700))
				foreign := t.TempDir()
				marker := filepath.Join(foreign, "keep.txt")
				require.NoError(t, os.WriteFile(marker, []byte("keep"), 0600))

				switch kind {
				case "file":
					require.NoError(t, os.WriteFile(link, []byte("original"), 0600))
				case "directory":
					require.NoError(t, os.Mkdir(link, 0700))
					require.NoError(t, os.WriteFile(filepath.Join(link, "keep.txt"), []byte("original"), 0600))
				case "link":
					require.NoError(t, junction.Create(foreign, link))
				case "dangling link":
					missing := filepath.Join(foreign, "missing")
					require.NoError(t, os.Mkdir(missing, 0700))
					require.NoError(t, junction.Create(missing, link))
					require.NoError(t, os.Remove(missing))
				}

				before, err := os.Lstat(link)
				require.NoError(t, err)
				outcomes, err := cmd.reconcileSymlinks([]string{"hello"})
				require.NoError(t, err)
				require.Len(t, outcomes, 1)
				require.NoError(t, outcomes[0].Err)
				assert.FileExists(t, marker, "replacing a link must never delete its target")

				if policy.replace {
					want := linkStatusLinked
					if strings.Contains(kind, "link") {
						want = linkStatusRelinked
					}

					assert.Equal(t, want, outcomes[0].Status)

					got, err := os.Stat(link)
					require.NoError(t, err)
					wantInfo, err := os.Stat(target)
					require.NoError(t, err)
					assert.True(t, os.SameFile(got, wantInfo))
				} else {
					assert.Equal(t, linkStatusSkipped, outcomes[0].Status)

					after, err := os.Lstat(link)
					require.NoError(t, err)
					assert.True(t, os.SameFile(before, after))
					assert.Contains(t, output.String(), link)
				}

				assert.Equal(t, policy.interactive, strings.Contains(output.String(), "[y/N]"))
			})
		}
	}
}

func TestSymlinkContinuesAfterFailureOrDecline(t *testing.T) {
	cmd, entry, _ := newLinkTestCommand(t)
	cmd.Interactive = true
	cmd.stdin = strings.NewReader("n\ny\n")

	for _, name := range []string{"declined", "approved"} {
		require.NoError(t, os.Mkdir(filepath.Join(cmd.destDir, name), 0700))
		require.NoError(t, os.WriteFile(filepath.Join(entry, name), []byte("original"), 0600))
	}

	// A component longer than either platform permits produces a real
	// per-entry filesystem error without relying on permissions.
	bad := strings.Repeat("x", 300)
	outcomes, err := cmd.reconcileSymlinks([]string{bad, "declined", "approved"})
	require.NoError(t, err)
	require.Len(t, outcomes, 3)
	assert.Equal(t, linkStatusFailed, outcomes[0].Status)
	require.Error(t, outcomes[0].Err)
	assert.Contains(t, outcomes[0].Err.Error(), bad)
	assert.Equal(t, linkStatusSkipped, outcomes[1].Status)
	assert.Equal(t, linkStatusLinked, outcomes[2].Status)
	require.Error(t, joinLinkErrors(outcomes))
}

func TestSymlinkRelativeTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("relative POSIX symlink fixture; junctions use absolute targets")
	}

	cmd, entry, _ := newLinkTestCommand(t)
	target := filepath.Join(cmd.destDir, "hello")
	require.NoError(t, os.Mkdir(target, 0700))
	relative, err := filepath.Rel(entry, target)
	require.NoError(t, err)
	require.NoError(t, os.Symlink(relative, filepath.Join(entry, "hello")))

	outcomes, err := cmd.reconcileSymlinks([]string{"hello"})
	require.NoError(t, err)
	assert.Equal(t, linkStatusUnchanged, outcomes[0].Status)

	require.NoError(t, os.Remove(target))

	outcomes, err = cmd.pruneDanglingLinks()
	require.NoError(t, err)
	require.Len(t, outcomes, 1)
	assert.Equal(t, linkStatusRemoved, outcomes[0].Status)
}

func TestSymlinkConfirmationConsumesOneWholeLine(t *testing.T) {
	cmd, entry, _ := newLinkTestCommand(t)
	cmd.Interactive = true
	cmd.override = true
	cmd.stdin = strings.NewReader("yes extra yes\nn\ny\n")

	for _, name := range []string{"malformed", "declined", "approved"} {
		require.NoError(t, os.Mkdir(filepath.Join(cmd.destDir, name), 0700))
		require.NoError(t, os.WriteFile(filepath.Join(entry, name), []byte("mine"), 0600))
	}

	outcomes, err := cmd.reconcileSymlinks([]string{"malformed", "declined", "approved"})
	require.NoError(t, err)
	require.Len(t, outcomes, 3)
	assert.Equal(t, linkStatusSkipped, outcomes[0].Status)
	assert.Equal(t, linkStatusSkipped, outcomes[1].Status)
	assert.Equal(t, linkStatusLinked, outcomes[2].Status)
}

func TestPruneDanglingLinks(t *testing.T) {
	cmd, entry, _ := newLinkTestCommand(t)
	cmd.Interactive = true
	cmd.override = true

	foreign := t.TempDir()
	for _, tc := range []struct {
		name     string
		target   string
		dangling bool
	}{
		{name: "old", target: filepath.Join(cmd.destDir, "old"), dangling: true},
		{name: "live", target: filepath.Join(cmd.destDir, "live")},
		{name: "external", target: filepath.Join(foreign, "external"), dangling: true},
		{name: "nested", target: filepath.Join(cmd.destDir, "nested", "skill"), dangling: true},
	} {
		require.NoError(t, os.MkdirAll(tc.target, 0700))
		require.NoError(t, junction.Create(tc.target, filepath.Join(entry, tc.name)))

		if tc.dangling {
			require.NoError(t, os.Remove(tc.target))
		}
	}

	require.NoError(t, os.Mkdir(filepath.Join(entry, "manual"), 0700))

	outcomes, err := cmd.pruneDanglingLinks()
	require.NoError(t, err)
	require.Len(t, outcomes, 1)
	assert.Equal(t, "old", outcomes[0].Name)
	assert.Equal(t, linkStatusRemoved, outcomes[0].Status)

	_, err = os.Lstat(filepath.Join(entry, "old"))
	assert.ErrorIs(t, err, fs.ErrNotExist)

	for _, name := range []string{"live", "external", "nested", "manual"} {
		_, err = os.Lstat(filepath.Join(entry, name))
		require.NoError(t, err, name)
	}

	outcomes, err = cmd.pruneDanglingLinks()
	require.NoError(t, err)
	assert.Empty(t, outcomes)
}

func TestSymlinkDirectoryFailure(t *testing.T) {
	cmd, entry, _ := newLinkTestCommand(t)
	require.NoError(t, os.Remove(entry))
	require.NoError(t, os.WriteFile(entry, []byte("not a directory"), 0600))

	_, err := cmd.reconcileSymlinks([]string{"hello"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "personal skills directory")

	_, err = cmd.pruneDanglingLinks()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "personal skills directory")
	assert.False(t, errors.Is(err, fs.ErrNotExist))
}
