package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/alecthomas/kong"
	"github.com/creativeprojects/go-selfupdate"
	"github.com/google/go-github/v74/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type (
	updaterStub struct {
		release        *selfupdate.Release
		repository     selfupdate.Repository
		detectErr      error
		updateErr      error
		updatedRelease *selfupdate.Release
		updatedPath    string
		detectCalls    int
		updateCalls    int
		found          bool
	}

	releaseSource struct {
		releases  []selfupdate.SourceRelease
		assets    map[int64][]byte
		downloads []int64
	}
)

// TestCommandRunVerifiedUpdate exercises the real updater, archive extraction,
// checksum validator, and file replacement without making network requests.
func TestCommandRunVerifiedUpdate(t *testing.T) {
	cases := []struct {
		name      string
		wantError string
		check     bool
		corrupt   bool
		missing   bool
	}{
		{name: "install verified release"},
		{name: "check leaves file untouched", check: true},
		{name: "checksum mismatch preserves executable", corrupt: true, wantError: "checksum"},
		{name: "asset download failure preserves executable", missing: true, wantError: "asset unavailable"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source, updater, release := newReleaseFixture(t)

			binaryName := "jarvis-registry"
			if runtime.GOOS == "windows" {
				binaryName += ".exe"
			}

			oldBytes := readFixture(t, "old-binary.txt")
			newBytes := readFixture(t, "new-binary.txt")
			exe := filepath.Join(t.TempDir(), binaryName)
			require.NoError(t, os.WriteFile(exe, oldBytes, 0o600))
			before, err := os.Stat(exe)
			require.NoError(t, err)

			archive := makeArchive(t, release.AssetName, binaryName, newBytes)
			checksum := fmt.Sprintf("%x  %s\n", sha256.Sum256(archive), release.AssetName)
			source.assets[release.AssetID] = archive

			source.assets[release.ValidationAssetID] = []byte(checksum)
			if tc.corrupt {
				source.assets[release.AssetID] = []byte("corrupted download")
			}

			if tc.missing {
				delete(source.assets, release.AssetID)
			}

			out := &bytes.Buffer{}
			cmd := testCommand(t, updater, out)
			cmd.Check = tc.check
			cmd.executablePathFunc = func() (string, error) { return exe, nil }

			err = cmd.Run()
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				assert.Empty(t, out.String(), "failures must not report success")
			} else {
				require.NoError(t, err)
				assert.Contains(t, out.String(), "1.2.3")
			}

			afterBytes, err := os.ReadFile(exe)
			require.NoError(t, err)
			after, err := os.Stat(exe)
			require.NoError(t, err)

			if tc.check || tc.wantError != "" {
				assert.Equal(t, sha256.Sum256(oldBytes), sha256.Sum256(afterBytes))
				assert.Equal(t, before.ModTime(), after.ModTime())
			} else {
				assert.Equal(t, newBytes, afterBytes)
				assert.Equal(t, []int64{release.AssetID, release.ValidationAssetID}, source.downloads)
			}

			if tc.check {
				assert.Empty(t, source.downloads, "--check must not download either asset")

				_, err := os.Stat(filepath.Join(filepath.Dir(exe), "."+filepath.Base(exe)+".update.lock"))
				require.ErrorIs(t, err, os.ErrNotExist, "--check must not create a lock file")
			}
		})
	}
}

func TestCommandRun(t *testing.T) {
	_, _, release := newReleaseFixture(t)
	cases := []struct {
		detectErr   error
		updateErr   error
		name        string
		version     string
		wantOutput  string
		wantError   string
		wantUpdates int
		check       bool
		notFound    bool
		nilRelease  bool
	}{
		{name: "already latest", version: "1.2.3", wantOutput: "already up to date"},
		{name: "already latest check", version: "1.2.3", check: true, wantOutput: "already up to date"},
		{name: "v prefix normalized", version: "v1.2.3", wantOutput: "already up to date"},
		{name: "check newer", version: "1.0.0", check: true, wantOutput: "1.2.3 is available"},
		{name: "install newer", version: "v1.0.0", wantOutput: "Updated jarvis-registry to 1.2.3", wantUpdates: 1},
		{name: "upgrade prerelease to stable", version: "1.2.3-rc.1", wantOutput: "Updated", wantUpdates: 1},
		{name: "no downgrade", version: "2.0.0", wantOutput: "no downgrade performed"},
		{name: "no downgrade check", version: "2.0.0", check: true, wantOutput: "no downgrade performed"},
		{name: "release not found", version: "1.0.0", notFound: true, wantError: "no compatible CLI release"},
		{name: "nil release", version: "1.0.0", nilRelease: true, wantError: "no compatible CLI release"},
		{name: "GitHub rate limit", version: "1.0.0", detectErr: errors.New("API rate limit exceeded"), wantError: "API rate limit exceeded"},
		{name: "network error", version: "1.0.0", detectErr: errors.New("network unreachable"), wantError: "could not check for a CLI release"},
		{name: "update fails", version: "1.0.0", updateErr: errors.New("permission denied"), wantError: "check write permissions", wantUpdates: 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			updater := &updaterStub{release: release, found: !tc.notFound, detectErr: tc.detectErr, updateErr: tc.updateErr}
			if tc.nilRelease {
				updater.release = nil
			}

			out := &bytes.Buffer{}
			cmd := testCommand(t, updater, out)
			cmd.currentVersion = tc.version
			cmd.Check = tc.check
			expectedPath, err := cmd.executablePathFunc()
			require.NoError(t, err)

			err = cmd.Run()
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				assert.Empty(t, out.String())
			} else {
				require.NoError(t, err)
				assert.Contains(t, out.String(), tc.wantOutput)
			}

			assert.Equal(t, 1, updater.detectCalls)
			assert.Equal(t, tc.wantUpdates, updater.updateCalls)
			owner, repo, err := updater.repository.GetSlug()
			require.NoError(t, err)
			assert.Equal(t, "ascending-llc", owner)
			assert.Equal(t, "jarvis-registry-cli", repo, "must not use the registry mirror")

			if tc.wantUpdates > 0 {
				assert.Equal(t, expectedPath, updater.updatedPath)
				assert.Same(t, release, updater.updatedRelease)
			}
		})
	}
}

func TestCommandRunGuards(t *testing.T) {
	cases := []struct {
		pathError error
		name      string
		version   string
		path      string
		wantError string
	}{
		{name: "dev build", version: "dev", wantError: "go install"},
		{name: "invalid version", version: "snapshot", wantError: "invalid version"},
		{name: "missing version", wantError: "invalid version"},
		{name: "resolve failure", version: "1.0.0", pathError: errors.New("broken symlink"), wantError: "could not resolve"},
		{name: "Apple Silicon Homebrew", version: "1.0.0", path: "/opt/homebrew/bin/jarvis-registry", wantError: "brew upgrade jarvis-registry"},
		{name: "Intel Cellar", version: "1.0.0", path: "/usr/local/Cellar/jarvis-registry/1.0.0/bin/jarvis-registry", wantError: "brew upgrade jarvis-registry"},
		{name: "Linuxbrew", version: "1.0.0", path: "/home/linuxbrew/.linuxbrew/bin/jarvis-registry", wantError: "brew upgrade jarvis-registry"},
		{name: "case insensitive", version: "1.0.0", path: "/OPT/HOMEBREW/bin/jarvis-registry", wantError: "brew upgrade jarvis-registry"},
	}

	for _, tc := range cases {
		for _, check := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/check=%t", tc.name, check), func(t *testing.T) {
				updater := &updaterStub{}
				out := &bytes.Buffer{}
				cmd := testCommand(t, updater, out)
				cmd.currentVersion = tc.version
				cmd.Check = check
				cmd.executablePathFunc = func() (string, error) {
					return tc.path, tc.pathError
				}

				err := cmd.Run()

				require.ErrorContains(t, err, tc.wantError)
				assert.Zero(t, updater.detectCalls, "guards must run before any network request")
				assert.Zero(t, updater.updateCalls)
				assert.Empty(t, out.String())
			})
		}
	}
}

func TestCommandKongLifecycle(t *testing.T) {
	for _, check := range []bool{false, true} {
		t.Run(fmt.Sprintf("check=%t", check), func(t *testing.T) {
			var cli struct {
				Update Command `cmd:""`
			}

			parser, err := kong.New(&cli, kong.Vars{"version": "1.2.3"})
			require.NoError(t, err)

			args := []string{"update"}
			if check {
				args = append(args, "--check")
			}

			ctx, err := parser.Parse(args)
			require.NoError(t, err)
			assert.Equal(t, check, cli.Update.Check)
			assert.Equal(t, "1.2.3", cli.Update.currentVersion)
			assert.IsType(t, &log.Logger{}, cli.Update.logger)
			assert.IsType(t, &log.Logger{}, cli.Update.stderrLogger)
			assert.IsType(t, &selfupdate.Updater{}, cli.Update.updater)
			exe, err := cli.Update.executablePathFunc()
			require.NoError(t, err)
			assert.True(t, filepath.IsAbs(exe))

			_, _, release := newReleaseFixture(t)
			updater := &updaterStub{release: release, found: true}
			out := &bytes.Buffer{}
			cli.Update.updater = updater
			cli.Update.logger = log.New(out, "", 0)
			cli.Update.executablePathFunc = func() (string, error) { return filepath.Join("resolved", "jarvis-registry"), nil }

			require.NoError(t, ctx.Run())
			assert.Contains(t, out.String(), "already up to date")
			assert.Zero(t, updater.updateCalls)
		})
	}
}

func TestCommandAllowsUnmanagedPaths(t *testing.T) {
	_, _, release := newReleaseFixture(t)
	for _, exe := range []string{
		"/usr/local/bin/jarvis-registry",
		"/home/user/.local/bin/jarvis-registry",
		"/home/user/homebrew-tools/jarvis-registry",
		`C:\Program Files\Jarvis Registry\jarvis-registry.exe`,
	} {
		t.Run(exe, func(t *testing.T) {
			updater := &updaterStub{release: release, found: true}
			cmd := testCommand(t, updater, &bytes.Buffer{})
			cmd.executablePathFunc = func() (string, error) { return exe, nil }
			cmd.Check = true

			require.NoError(t, cmd.Run())
			assert.Equal(t, 1, updater.detectCalls)
			assert.Zero(t, updater.updateCalls)
		})
	}
}

func TestCommandMissingChecksum(t *testing.T) {
	source, _, _ := newReleaseFixture(t)
	info := readReleaseInfo(t)
	info.Assets = info.Assets[:len(info.Assets)-1]
	source.releases = []selfupdate.SourceRelease{selfupdate.NewGitHubRelease(info)}
	updater, err := newUpdater(source)
	require.NoError(t, err)

	cmd := testCommand(t, updater, &bytes.Buffer{})

	err = cmd.Run()

	require.ErrorContains(t, err, "checksums.txt")
	assert.Empty(t, source.downloads)
}

func TestReleaseSelection(t *testing.T) {
	for _, goos := range []string{"darwin", "linux", "windows"} {
		for _, arch := range []string{"amd64", "arm64"} {
			t.Run(goos+"/"+arch, func(t *testing.T) {
				info := readReleaseInfo(t)
				prerelease := readReleaseInfo(t)
				prerelease.TagName = github.Ptr("v2.0.0-rc.1")
				prerelease.Prerelease = github.Ptr(true)
				draft := readReleaseInfo(t)
				draft.TagName = github.Ptr("v3.0.0")
				draft.Draft = github.Ptr(true)
				source := &releaseSource{releases: []selfupdate.SourceRelease{
					selfupdate.NewGitHubRelease(draft), selfupdate.NewGitHubRelease(prerelease), selfupdate.NewGitHubRelease(info),
				}}
				updater, err := selfupdate.NewUpdater(selfupdate.Config{
					Source: source, OS: goos, Arch: arch,
					Validator: &selfupdate.ChecksumValidator{UniqueFilename: "checksums.txt"},
				})
				require.NoError(t, err)

				release, found, err := updater.DetectLatest(t.Context(), selfupdate.NewRepositorySlug("ascending-llc", "jarvis-registry-cli"))

				require.NoError(t, err)
				require.True(t, found)
				assert.Equal(t, "1.2.3", release.Version())
				assert.Contains(t, release.AssetName, "_"+goos+"_"+arch)
				assert.EqualValues(t, 7, release.ValidationAssetID)
				assert.Empty(t, source.downloads)
			})
		}
	}
}

func TestCommandUpdateKeepsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks requires additional Windows privileges")
	}

	source, updater, release := newReleaseFixture(t)
	exe := filepath.Join(t.TempDir(), "jarvis-registry")
	link := filepath.Join(filepath.Dir(exe), "jr")
	require.NoError(t, os.WriteFile(exe, readFixture(t, "old-binary.txt"), 0o600))
	require.NoError(t, os.Symlink("jarvis-registry", link))
	newBytes := readFixture(t, "new-binary.txt")
	archive := makeArchive(t, release.AssetName, "jarvis-registry", newBytes)
	source.assets[release.AssetID] = archive
	source.assets[release.ValidationAssetID] = fmt.Appendf(nil, "%x  %s\n", sha256.Sum256(archive), release.AssetName)
	cmd := testCommand(t, updater, &bytes.Buffer{})
	cmd.executablePathFunc = func() (string, error) { return filepath.EvalSymlinks(link) }

	require.NoError(t, cmd.Run())

	target, err := os.Readlink(link)
	require.NoError(t, err)
	assert.Equal(t, "jarvis-registry", target)

	actual, err := os.ReadFile(link)
	require.NoError(t, err)
	assert.Equal(t, newBytes, actual)
}

// TestCommandReplacesRunningExecutable runs a copy of this test executable as
// a child and makes it replace itself. Windows CI exercises the live .exe case.
func TestCommandReplacesRunningExecutable(t *testing.T) {
	const helperKey = "JARVIS_SELFUPDATE_TEST_HELPER"
	if os.Getenv(helperKey) == "1" {
		source, updater, release := newReleaseFixture(t)
		exe, err := selfupdate.ExecutablePath()
		require.NoError(t, err)
		archive := makeArchive(t, release.AssetName, filepath.Base(exe), readFixture(t, "new-binary.txt"))
		source.assets[release.AssetID] = archive
		source.assets[release.ValidationAssetID] = fmt.Appendf(nil, "%x  %s\n", sha256.Sum256(archive), release.AssetName)
		cmd := testCommand(t, updater, &bytes.Buffer{})
		cmd.executablePathFunc = selfupdate.ExecutablePath
		require.NoError(t, cmd.Run())

		return
	}

	original, err := os.Executable()
	require.NoError(t, err)
	content, err := os.ReadFile(original)
	require.NoError(t, err)

	name := "jarvis-registry"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}

	copyPath := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(copyPath, content, 0o700))

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	child := exec.CommandContext(ctx, copyPath, "-test.run=^TestCommandReplacesRunningExecutable$")

	child.Env = append(os.Environ(), helperKey+"=1")
	output, err := child.CombinedOutput()
	require.NoError(t, err, "%s", output)
	updated, err := os.ReadFile(copyPath)
	require.NoError(t, err)
	assert.Equal(t, readFixture(t, "new-binary.txt"), updated)
}

func (u *updaterStub) DetectLatest(_ context.Context, repo selfupdate.Repository) (*selfupdate.Release, bool, error) {
	u.detectCalls++
	u.repository = repo

	return u.release, u.found, u.detectErr
}

func (u *updaterStub) UpdateTo(_ context.Context, release *selfupdate.Release, cmdPath string) error {
	u.updateCalls++
	u.updatedRelease = release
	u.updatedPath = cmdPath

	return u.updateErr
}

func (s *releaseSource) ListReleases(_ context.Context, _ selfupdate.Repository) ([]selfupdate.SourceRelease, error) {
	return s.releases, nil
}

func (s *releaseSource) DownloadReleaseAsset(_ context.Context, _ *selfupdate.Release, assetID int64) (io.ReadCloser, error) {
	s.downloads = append(s.downloads, assetID)

	data, ok := s.assets[assetID]
	if !ok {
		return nil, errors.New("asset unavailable")
	}

	return io.NopCloser(bytes.NewReader(data)), nil
}

func testCommand(t *testing.T, updater Updater, out *bytes.Buffer) Command {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "jarvis-registry")

	return Command{
		logger:             log.New(out, "", 0),
		stderrLogger:       log.New(out, "", 0),
		updater:            updater,
		currentVersion:     "1.0.0",
		executablePathFunc: func() (string, error) { return exe, nil },
	}
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)

	return data
}

func readReleaseInfo(t *testing.T) *github.RepositoryRelease {
	t.Helper()

	var info github.RepositoryRelease
	require.NoError(t, json.Unmarshal(readFixture(t, "release.json"), &info))

	return &info
}

func newReleaseFixture(t *testing.T) (*releaseSource, *selfupdate.Updater, *selfupdate.Release) {
	t.Helper()
	source := &releaseSource{
		releases: []selfupdate.SourceRelease{selfupdate.NewGitHubRelease(readReleaseInfo(t))},
		assets:   make(map[int64][]byte),
	}
	updater, err := newUpdater(source)
	require.NoError(t, err)
	release, found, err := updater.DetectLatest(t.Context(), selfupdate.NewRepositorySlug("ascending-llc", "jarvis-registry-cli"))
	require.NoError(t, err)
	require.True(t, found)

	return source, updater, release
}

func makeArchive(t *testing.T, assetName, binaryName string, content []byte) []byte {
	t.Helper()

	var buffer bytes.Buffer

	if strings.HasSuffix(assetName, ".zip") {
		writer := zip.NewWriter(&buffer)
		file, err := writer.Create(binaryName)
		require.NoError(t, err)
		_, err = file.Write(content)
		require.NoError(t, err)
		require.NoError(t, writer.Close())

		return buffer.Bytes()
	}

	compressed := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(compressed)
	require.NoError(t, writer.WriteHeader(&tar.Header{
		Name: binaryName, Mode: 0o755, Size: int64(len(content)), ModTime: time.Unix(0, 0),
	}))
	_, err := writer.Write(content)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	require.NoError(t, compressed.Close())

	return buffer.Bytes()
}
