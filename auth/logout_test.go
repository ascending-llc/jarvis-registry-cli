package auth

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"

	"github.com/ascending-llc/jarvis-registry-cli/cfg"
	"github.com/ascending-llc/jarvis-registry-cli/creds"
)

func TestLogoutCommandBeforeReset(t *testing.T) {
	cmd := &LogoutCommand{}
	require.NoError(t, cmd.BeforeReset())
	logger, ok := cmd.logger.(*log.Logger)
	require.True(t, ok)
	assert.Empty(t, logger.Prefix())
	assert.NotNil(t, cmd.exitFunc)
	assert.NotNil(t, cmd.configLoadFunc)
}

func TestAuthCommandLifecycle(t *testing.T) {
	for _, mode := range []string{"keyring", "file only", "linux fallback"} {
		t.Run(mode, func(t *testing.T) {
			if mode == "linux fallback" && runtime.GOOS != "linux" {
				t.Skip("automatic fallback is Linux-only")
			}

			keyring.MockInit()

			if mode != "keyring" {
				keyring.MockInitWithError(errors.New("keyring backend unavailable"))
			}

			ts := newRealPathAuthServer(t, deviceCodeHandler(t), refreshHandler(t, failIfCalled(t, "refresh")))
			defer ts.Close()

			home := mockUserHomeDir(t)
			dir := filepath.Join(home, cfg.RegistryDirName)
			require.NoError(t, os.MkdirAll(dir, 0o700))

			config := fmt.Sprintf("registry:\n  base_url: https://registry.example.com\n  auth_base_url: %s\nlocal:\n  credentials:\n    file_only: %t\n", ts.URL, mode == "file only")
			require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(config), 0o600))

			var out bytes.Buffer

			login := &LoginCommand{}
			require.NoError(t, login.BeforeReset())
			login.logger = log.New(&out, "", 0)
			require.NoError(t, login.AfterApply())
			stubDeviceFlowInteraction(&login.resolver)
			require.NoError(t, login.Run())
			assert.Contains(t, out.String(), "✓ Logged in")

			token, err := login.resolver.GetAccessToken()
			require.NoError(t, err)
			assert.Equal(t, "device-access-token", token)

			status := &StatusCommand{}
			require.NoError(t, status.BeforeReset())
			status.logger = log.New(&out, "", 0)
			statusExit := -1
			status.exitFunc = func(code int) { statusExit = code }
			require.NoError(t, status.AfterApply())
			out.Reset()
			require.NoError(t, status.Run())
			assert.Equal(t, -1, statusExit)

			location := filepath.Join(dir, creds.CredentialsFileName)
			if mode == "keyring" {
				assert.NoFileExists(t, location)
				location = "keyring"
			}

			assert.Contains(t, out.String(), "✓ Logged in ("+location+")")

			// Linux fallback logout also removes a stale keyring entry; file-only
			// logout never touches the keyring, so the entry survives.
			if mode != "keyring" {
				keyring.MockInit()
				require.NoError(t, keyring.Set(jarvisRegistryService+":"+ts.URL, jarvisRegistryCli, "stale"))
			}

			logout := &LogoutCommand{}
			require.NoError(t, logout.BeforeReset())
			logout.logger = log.New(&out, "", 0)
			logoutExit := -1
			logout.exitFunc = func(code int) { logoutExit = code }
			require.NoError(t, logout.AfterApply())
			out.Reset()
			require.NoError(t, logout.Run())
			assert.Equal(t, -1, logoutExit)
			assert.Equal(t, "✓ Logged out of https://registry.example.com\n", out.String())

			_, err = logout.resolver.creds.Read()
			require.ErrorIs(t, err, creds.ErrCredentialsNotExist)

			stale, err := keyring.Get(jarvisRegistryService+":"+ts.URL, jarvisRegistryCli)
			if mode == "file only" {
				require.NoError(t, err)
				assert.Equal(t, "stale", stale)
			} else {
				require.ErrorIs(t, err, keyring.ErrNotFound)
			}

			require.NoError(t, status.Run())
			assert.Equal(t, 1, statusExit)

			out.Reset()
			require.NoError(t, logout.Run())
			assert.Equal(t, 1, logoutExit)
			assert.Equal(t, "✗ Not logged in to https://registry.example.com. Run `jarvis-registry auth login` to authenticate.\n", out.String())
		})
	}
}

func TestLogoutCommandBackendFailure(t *testing.T) {
	for _, tc := range []struct {
		name     string
		fileOnly bool
		cached   bool
	}{
		{name: "default missing"},
		{name: "default cached", cached: true},
		{name: "file-only missing", fileOnly: true},
		{name: "file-only cached", fileOnly: true, cached: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			keyring.MockInitWithError(errors.New("keyring backend unavailable"))

			dir := t.TempDir()

			const baseURL = "https://registry.example.com"

			file := creds.NewFileReadWriter(filepath.Join(dir, creds.CredentialsFileName), jarvisRegistryService+":"+baseURL)
			if tc.cached {
				require.NoError(t, file.Write([]byte("value")))
			}

			var out bytes.Buffer

			cmd := &LogoutCommand{
				baseUrl:  baseURL,
				logger:   log.New(&out, "", 0),
				resolver: NewRegistryTokenResolver(baseURL, RegistryScopes, dir, tc.fileOnly, log.New(&out, "", 0)),
			}
			exitCode := -1
			cmd.exitFunc = func(code int) { exitCode = code }

			err := cmd.Run()

			assert.NoFileExists(t, file.Location())

			// File-only mode never calls the failing keyring; Linux fallback ignores it.
			if runtime.GOOS == "linux" || tc.fileOnly {
				require.NoError(t, err)

				if tc.cached {
					assert.Equal(t, -1, exitCode)
					assert.Equal(t, "✓ Logged out of "+baseURL+"\n", out.String())
				} else {
					assert.Equal(t, 1, exitCode)
					assert.Contains(t, out.String(), "✗ Not logged in to "+baseURL)
				}

				return
			}

			require.ErrorContains(t, err, "failed to log out of the Registry")
			assert.Contains(t, err.Error(), "keyring backend unavailable")
			assert.Equal(t, -1, exitCode)
			assert.Empty(t, out.String(), "failed cleanup must not print a successful logout or cache miss")
		})
	}
}
