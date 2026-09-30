package auth

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"

	"github.com/ascending-llc/jarvis-registry-cli/cfg"
)

func TestLoginCommandBeforeReset(t *testing.T) {
	cmd := &LoginCommand{}

	err := cmd.BeforeReset()
	require.NoError(t, err, "should be able to call LoginCommand.BeforeReset without error")

	logger, ok := cmd.logger.(*log.Logger)
	require.True(t, ok, "logger should be a *log.Logger")
	assert.Empty(t, logger.Prefix(), "logger should not have a prefix, so its output isn't run together with unprefixed messages")
}

func TestLoginCommandRun(t *testing.T) {
	t.Run("no cached credentials runs the device flow and caches the resulting tokens", func(t *testing.T) {
		keyring.MockInit()

		ts := newRealPathAuthServer(t, deviceCodeHandler(t), refreshHandler(t, failIfCalled(t, "refresh")))
		defer ts.Close()

		cmd := newTestLoginCommand(t, ts.URL)

		err := cmd.Run()
		require.NoError(t, err, "Run should succeed via the device flow when no credentials are stored")

		assertStoredAccessToken(t, cmd.resolver.creds, "device-access-token")
	})

	t.Run("valid cached token is a no-op", func(t *testing.T) {
		keyring.MockInit()

		ts := newRealPathAuthServer(t, failIfCalled(t, "device code"), failIfCalled(t, "refresh"))
		defer ts.Close()

		cmd := newTestLoginCommand(t, ts.URL)

		seeded := StoredTokens{
			LastUpdate:   time.Now().UTC(),
			AccessToken:  "cached-access-token",
			RefreshToken: "cached-refresh-token",
			Scope:        testScope,
		}

		seedStoredTokens(t, cmd.resolver.creds, seeded)

		err := cmd.Run()
		require.NoError(t, err, "Run should be a no-op when a non-expired token is already stored")

		assert.Equal(t, seeded, readStoredTokens(t, cmd.resolver.creds), "Run should not have rewritten the stored credentials")
	})

	t.Run("expired token with working refresh refreshes without running the device flow", func(t *testing.T) {
		keyring.MockInit()

		ts := newRealPathAuthServer(t, failIfCalled(t, "device code"), refreshHandler(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSONTokenResponse(t, w, http.StatusOK, "new-access-token", "new-refresh-token")
		}))
		defer ts.Close()

		cmd := newTestLoginCommand(t, ts.URL)

		seedStoredTokens(t, cmd.resolver.creds, StoredTokens{
			LastUpdate:   time.Now().UTC().Add(-2 * time.Hour),
			AccessToken:  "old-access-token",
			RefreshToken: "old-refresh-token",
		})

		err := cmd.Run()
		require.NoError(t, err, "Run should succeed when the refresh flow succeeds")

		assertStoredAccessToken(t, cmd.resolver.creds, "new-access-token")
	})

	t.Run("expired token with failed refresh falls back to the device flow", func(t *testing.T) {
		keyring.MockInit()

		ts := newRealPathAuthServer(t, deviceCodeHandler(t), refreshHandler(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSONErrorResponse(t, w, http.StatusBadRequest, "invalid_grant", "refresh token expired")
		}))
		defer ts.Close()

		cmd := newTestLoginCommand(t, ts.URL)

		seedStoredTokens(t, cmd.resolver.creds, StoredTokens{
			LastUpdate:   time.Now().UTC().Add(-2 * time.Hour),
			AccessToken:  "old-access-token",
			RefreshToken: "old-refresh-token",
		})

		err := cmd.Run()
		require.NoError(t, err, "Run should fall back to the device flow when refresh fails")

		assertStoredAccessToken(t, cmd.resolver.creds, "device-access-token")
	})

	t.Run("device flow failure surfaces as a wrapped Run error", func(t *testing.T) {
		keyring.MockInit()

		ts := newRealPathAuthServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}, refreshHandler(t, failIfCalled(t, "refresh")))
		defer ts.Close()

		cmd := newTestLoginCommand(t, ts.URL)

		err := cmd.Run()
		require.Error(t, err, "Run should return an error when the device flow fails")
		assert.Contains(t, err.Error(), "failed to log in to the Registry", "error should be wrapped with the command's own context")
	})
}

func TestLoginCommandWithoutBrowser(t *testing.T) {
	for _, denied := range []bool{false, true} {
		name := "authorized after pending"
		if denied {
			name = "authorization denied"
		}

		t.Run(name, func(t *testing.T) {
			// Exercise the real browser callback without starting a desktop browser.
			t.Setenv("PATH", t.TempDir())

			var tokenRequests atomic.Int32

			ts := newRealPathAuthServer(t, deviceCodeHandler(t), func(w http.ResponseWriter, r *http.Request) {
				require.NoError(t, r.ParseForm())
				assert.Equal(t, "urn:ietf:params:oauth:grant-type:device_code", r.FormValue("grant_type"))
				assert.Equal(t, "DEVICE123", r.FormValue("device_code"))

				if tokenRequests.Add(1) == 1 {
					writeJSONErrorResponse(t, w, http.StatusBadRequest, "authorization_pending", "waiting for approval")

					return
				}

				if denied {
					writeJSONErrorResponse(t, w, http.StatusBadRequest, "access_denied", "authorization denied")

					return
				}

				writeJSONTokenResponse(t, w, http.StatusOK, "device-access-token", "device-refresh-token")
			})
			defer ts.Close()

			home := mockUserHomeDir(t)
			dir := filepath.Join(home, cfg.RegistryDirName)
			require.NoError(t, os.MkdirAll(dir, 0o700))

			config := fmt.Sprintf("registry:\n  base_url: %s\nlocal:\n  credentials:\n    file_only: true\n", ts.URL)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(config), 0o600))

			var out bytes.Buffer

			cmd := &LoginCommand{}
			require.NoError(t, cmd.BeforeReset())
			cmd.logger = log.New(&out, "", 0)
			require.NoError(t, cmd.AfterApply())
			cmd.resolver.flow.Stdout = &out

			cmd.resolver.flow.Stdin = strings.NewReader("") // Headless stdin is already at EOF.
			if denied {
				cmd.resolver.flow.Stdin = strings.NewReader("\n")
			}

			err := cmd.Run()

			assert.Equal(t, int32(2), tokenRequests.Load(), "browser failure must not stop polling")
			assert.Contains(t, out.String(), "USER-CODE")
			assert.Contains(t, out.String(), "Open http://example.com/verify in a browser and enter the code above")
			assert.Contains(t, out.String(), "Waiting for authorization")
			assert.NotContains(t, out.String(), "device-access-token")
			assert.NotContains(t, out.String(), "device-refresh-token")

			if denied {
				require.ErrorContains(t, err, "failed to log in to the Registry")
				assert.Contains(t, err.Error(), "access_denied")
				assert.NotContains(t, out.String(), "✓ Logged in")
				assert.NoFileExists(t, cmd.resolver.CredentialsLocation())

				return
			}

			require.NoError(t, err)
			assert.Contains(t, out.String(), "✓ Logged in")
			assert.FileExists(t, cmd.resolver.CredentialsLocation())
			assertStoredAccessToken(t, cmd.resolver.creds, "device-access-token")
		})
	}
}

// newRealPathAuthServer wires up a test server exposing the device/token
// endpoints at the same paths NewRegistryTokenResolver builds in
// production (deviceCodePath/tokenPath), unlike newAuthServer's
// abbreviated "/device" and "/token" routes used by tests that construct a
// RegistryTokenResolver by hand.
func newRealPathAuthServer(t *testing.T, deviceHandler, tokenHandler http.HandlerFunc) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("POST "+deviceCodePath, deviceHandler)
	mux.HandleFunc("POST "+tokenPath, tokenHandler)

	return httptest.NewServer(mux)
}

func newTestLoginCommand(t *testing.T, authBaseUrl string) *LoginCommand {
	t.Helper()

	mockUserHomeDir(t)

	cmd := &LoginCommand{}

	require.NoError(t, cmd.BeforeReset(), "should be able to call LoginCommand.BeforeReset without error")

	cmd.logger = log.New(io.Discard, "", 0)

	cmd.configLoadFunc = func(string) (cfg.Config, error) {
		var config cfg.Config

		config.Registry.AuthBaseUrl = authBaseUrl

		return config, nil
	}

	require.NoError(t, cmd.AfterApply(), "should be able to call LoginCommand.AfterApply without error")

	stubDeviceFlowInteraction(&cmd.resolver)

	return cmd
}

// stubDeviceFlowInteraction disables code prompts and browser launches for
// tests that do not exercise user interaction. Headless-login tests retain
// the production callbacks and supply the flow's stdin/stdout instead.
func stubDeviceFlowInteraction(r *RegistryTokenResolver) {
	r.flow.DisplayCode = func(string, string) error { return nil }
	r.flow.BrowseURL = func(string) error { return nil }
}
