package auth

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"

	"github.com/ascending-llc/jarvis-registry-cli/cfg"
)

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

	mockHomeDir := t.TempDir()

	originalHome := os.Getenv("HOME")
	originalUserProfile := os.Getenv("USERPROFILE")

	require.NoError(t, os.Setenv("HOME", mockHomeDir), "should be able to set HOME env var so os.UserHomeDir returns the mocked home directory")
	require.NoError(t, os.Setenv("USERPROFILE", mockHomeDir), "should be able to set USERPROFILE env var so os.UserHomeDir returns the mocked home directory on Windows")

	t.Cleanup(func() {
		_ = os.Setenv("HOME", originalHome)
		_ = os.Setenv("USERPROFILE", originalUserProfile)
	})

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

// stubDeviceFlowInteraction replaces the resolver's user-interaction
// callbacks with no-ops. NewRegistryTokenResolver leaves oauth.Flow's
// DisplayCode and BrowseURL nil, which is correct in production (the
// underlying library falls back to printing a code and waiting on stdin,
// then opening a real browser) but unusable in a test process — any test
// path that reaches the device flow needs these stubbed first.
func stubDeviceFlowInteraction(r *RegistryTokenResolver) {
	r.flow.DisplayCode = func(string, string) error { return nil }
	r.flow.BrowseURL = func(string) error { return nil }
}
