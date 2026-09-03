package auth

import (
	"bytes"
	"errors"
	"log"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"

	"github.com/ascending-llc/jarvis-registry-cli/cfg"
)

func TestStatusCommandRun(t *testing.T) {
	t.Run("always prints the configured registry.base_url regardless of login state", func(t *testing.T) {
		keyring.MockInit()

		ts := newRealPathAuthServer(t, failIfCalled(t, "device code"), failIfCalled(t, "refresh"))
		defer ts.Close()

		cmd, out := newTestStatusCommand(t, "https://registry.example.com", ts.URL)

		cmd.exitFunc = func(int) {}

		err := cmd.Run()
		require.NoError(t, err, "Run should not error for the not-logged-in path")

		assert.Contains(t, out.String(), "https://registry.example.com", "Run should print registry.base_url")
	})

	t.Run("valid cached token prints logged in and scopes, and exits 0", func(t *testing.T) {
		keyring.MockInit()

		ts := newRealPathAuthServer(t, failIfCalled(t, "device code"), failIfCalled(t, "refresh"))
		defer ts.Close()

		cmd, out := newTestStatusCommand(t, "https://registry.example.com", ts.URL)

		seedStoredTokens(t, cmd.resolver.creds, StoredTokens{
			LastUpdate:   time.Now().UTC(),
			AccessToken:  "cached-access-token",
			RefreshToken: "cached-refresh-token",
			Scope:        "skills-read other-scope",
		})

		var exitCode = -1

		cmd.exitFunc = func(code int) { exitCode = code }

		err := cmd.Run()
		require.NoError(t, err, "Run should not error when a valid token is cached")

		assert.Equal(t, -1, exitCode, "exitFunc should not be invoked on the logged-in path")
		assert.Contains(t, out.String(), "Logged in", "Run should print a logged-in line")
		assert.Contains(t, out.String(), "'skills-read', 'other-scope'", "Run should print the formatted token scopes")
	})

	t.Run("expired token with working refresh refreshes, re-caches, and reports logged in, and exits 0", func(t *testing.T) {
		keyring.MockInit()

		ts := newRealPathAuthServer(t, failIfCalled(t, "device code"), refreshHandler(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSONTokenResponse(t, w, http.StatusOK, "new-access-token", "new-refresh-token")
		}))
		defer ts.Close()

		cmd, out := newTestStatusCommand(t, "https://registry.example.com", ts.URL)

		seedStoredTokens(t, cmd.resolver.creds, StoredTokens{
			LastUpdate:   time.Now().UTC().Add(-2 * time.Hour),
			AccessToken:  "old-access-token",
			RefreshToken: "old-refresh-token",
		})

		var exitCode = -1

		cmd.exitFunc = func(code int) { exitCode = code }

		err := cmd.Run()
		require.NoError(t, err, "Run should not error when the refresh flow succeeds")

		assert.Equal(t, -1, exitCode, "exitFunc should not be invoked when refresh succeeds")
		assert.Contains(t, out.String(), "Logged in", "Run should print a logged-in line")

		assertStoredAccessToken(t, cmd.resolver.creds, "new-access-token")
	})

	t.Run("no cached credentials prints not logged in and exits 1", func(t *testing.T) {
		keyring.MockInit()

		ts := newRealPathAuthServer(t, failIfCalled(t, "device code"), failIfCalled(t, "refresh"))
		defer ts.Close()

		cmd, out := newTestStatusCommand(t, "https://registry.example.com", ts.URL)

		var exitCode = -1

		cmd.exitFunc = func(code int) { exitCode = code }

		err := cmd.Run()
		require.NoError(t, err, "Run should return nil for the not-logged-in path so kong does not print a redundant error")

		assert.Equal(t, 1, exitCode, "exitFunc should be invoked with 1 when not logged in")
		assert.Contains(t, out.String(), "Not logged in", "Run should print a not-logged-in line")
		assert.Contains(t, out.String(), "auth login", "Run should hint at the auth login command")
	})

	t.Run("expired token with failed refresh prints not logged in and exits 1", func(t *testing.T) {
		keyring.MockInit()

		ts := newRealPathAuthServer(t, failIfCalled(t, "device code"), refreshHandler(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSONErrorResponse(t, w, http.StatusBadRequest, "invalid_grant", "refresh token expired")
		}))
		defer ts.Close()

		cmd, out := newTestStatusCommand(t, "https://registry.example.com", ts.URL)

		seedStoredTokens(t, cmd.resolver.creds, StoredTokens{
			LastUpdate:   time.Now().UTC().Add(-2 * time.Hour),
			AccessToken:  "old-access-token",
			RefreshToken: "old-refresh-token",
		})

		var exitCode = -1

		cmd.exitFunc = func(code int) { exitCode = code }

		err := cmd.Run()
		require.NoError(t, err, "Run should return nil for the not-logged-in path so kong does not print a redundant error")

		assert.Equal(t, 1, exitCode, "exitFunc should be invoked with 1 when refresh fails")
		assert.Contains(t, out.String(), "Not logged in", "Run should print a not-logged-in line")
	})

	t.Run("unrelated keyring failure surfaces as a normal command error, not not-logged-in", func(t *testing.T) {
		ts := newRealPathAuthServer(t, failIfCalled(t, "device code"), failIfCalled(t, "refresh"))
		defer ts.Close()

		cmd, _ := newTestStatusCommand(t, "https://registry.example.com", ts.URL)

		keyring.MockInitWithError(errors.New("keyring backend unavailable"))

		var exitCode = -1

		cmd.exitFunc = func(code int) { exitCode = code }

		err := cmd.Run()
		require.Error(t, err, "Run should surface a keyring failure that is not ErrCredentialsNotExist as a real error")
		assert.Contains(t, err.Error(), "failed to check Registry authentication status", "error should be wrapped with the command's own context")

		assert.Equal(t, -1, exitCode, "exitFunc should not be invoked on an unrelated failure: the command error path handles the exit code")
	})
}

func TestFormatScopes(t *testing.T) {
	cases := []struct {
		name  string
		scope string
		want  string
	}{
		{name: "empty scope", scope: "", want: ""},
		{name: "single scope", scope: "skills-read", want: "'skills-read'"},
		{name: "multiple space-separated scopes", scope: "skills-read other-scope", want: "'skills-read', 'other-scope'"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, formatScopes(c.scope))
		})
	}
}

func newTestStatusCommand(t *testing.T, baseUrl, authBaseUrl string) (cmd *StatusCommand, out *bytes.Buffer) {
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

	cmd = &StatusCommand{}

	require.NoError(t, cmd.BeforeReset(), "should be able to call StatusCommand.BeforeReset without error")

	out = &bytes.Buffer{}
	cmd.logger = log.New(out, "", 0)

	cmd.configLoadFunc = func(string) (cfg.Config, error) {
		var config cfg.Config

		config.Registry.BaseUrl = baseUrl
		config.Registry.AuthBaseUrl = authBaseUrl

		return config, nil
	}

	require.NoError(t, cmd.AfterApply(), "should be able to call StatusCommand.AfterApply without error")

	return cmd, out
}
