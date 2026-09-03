package auth

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/cli/oauth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"

	"github.com/ascending-llc/jarvis-registry-cli/creds"
	registryHttp "github.com/ascending-llc/jarvis-registry-cli/internal/http"
)

const (
	testService = "test-service"
	testUser    = "test-user"

	// testScope is the fixed scope string the mocked token endpoint reports
	// on every successful device-flow and refresh-flow response.
	testScope = "skills-read"
)

func TestNewRegistryTokenResolver(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	scopes := []string{"registry:read", "registry:write"}

	r := NewRegistryTokenResolver("https://registry.example.com/", scopes, logger)

	assert.Equal(t, "https://registry.example.com/auth/oauth2/device/code", r.deviceCodeUrl, "deviceCodeUrl should be the trailing-slash-trimmed baseUrl plus deviceCodePath")
	assert.Equal(t, "https://registry.example.com/auth/oauth2/token", r.tokenUrl, "tokenUrl should be the trailing-slash-trimmed baseUrl plus tokenPath")
	assert.Equal(t, scopes, r.scopes, "scopes should be stored as given")

	require.NotNil(t, r.flow, "flow should be initialized")
	assert.Equal(t, clientId, r.flow.ClientID, "flow.ClientID should be the fixed client id")
	assert.Equal(t, scopes, r.flow.Scopes, "flow.Scopes should match the given scopes")
	assert.Equal(t, r.deviceCodeUrl, r.flow.Host.DeviceCodeURL, "flow.Host.DeviceCodeURL should match deviceCodeUrl")
	assert.Equal(t, r.tokenUrl, r.flow.Host.TokenURL, "flow.Host.TokenURL should match tokenUrl")
	assert.Same(t, registryHttp.DefaultClient, r.flow.HTTPClient, "flow.HTTPClient should be the shared, timeout-bounded internal/http.DefaultClient rather than the zero-value http.DefaultClient, so the device flow's HTTP calls cannot hang indefinitely on a stalled connection")
}

func TestNewRegistryTokenResolver_CredsScopedToBaseUrl(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	scopes := []string{"registry:read"}

	cases := []struct {
		name        string
		baseUrl     string
		wantService string
	}{
		{name: "trailing slash is trimmed before scoping", baseUrl: "https://registry.example.com/", wantService: "jarvis-registry:https://registry.example.com"},
		{name: "distinct baseUrl scopes to a distinct service", baseUrl: "http://localhost:8080", wantService: "jarvis-registry:http://localhost:8080"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			keyring.MockInit()

			r := NewRegistryTokenResolver(c.baseUrl, scopes, logger)

			require.NoError(t, r.creds.Write([]byte("probe")), "should be able to write through the resolver's creds")

			got, err := keyring.Get(c.wantService, jarvisRegistryCli)
			require.NoError(t, err, "the resolver's creds should be readable under the expected baseUrl-scoped service and the fixed jarvisRegistryCli account")
			assert.Equal(t, "probe", got, "the value written through the resolver's creds should round-trip under the expected service/account pair")
		})
	}
}

func TestGetAccessToken(t *testing.T) {
	t.Run("cached token still valid returns it without any network call", func(t *testing.T) {
		ts := newAuthServer(t, failIfCalled(t, "device code"), failIfCalled(t, "refresh"))
		defer ts.Close()

		r := newTestResolver(t, ts)

		seedStoredTokens(t, r.creds, StoredTokens{
			LastUpdate:   time.Now().UTC(),
			AccessToken:  "cached-access-token",
			RefreshToken: "cached-refresh-token",
		})

		token, err := r.GetAccessToken()
		require.NoError(t, err, "GetAccessToken should succeed when a non-expired token is already stored")

		assert.Equal(t, "cached-access-token", token, "GetAccessToken should return the cached access token as-is")
	})

	t.Run("expired token is refreshed and the new token is persisted", func(t *testing.T) {
		ts := newAuthServer(t, failIfCalled(t, "device code"), refreshHandler(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "old-refresh-token", r.FormValue("refresh_token"), "refresh request should carry the stored refresh token")
			assert.Equal(t, clientId, r.FormValue("client_id"), "refresh request should carry the fixed client id")

			writeJSONTokenResponse(t, w, http.StatusOK, "new-access-token", "new-refresh-token")
		}))
		defer ts.Close()

		r := newTestResolver(t, ts)

		seedStoredTokens(t, r.creds, StoredTokens{
			LastUpdate:   time.Now().UTC().Add(-2 * time.Hour),
			AccessToken:  "old-access-token",
			RefreshToken: "old-refresh-token",
		})

		token, err := r.GetAccessToken()
		require.NoError(t, err, "GetAccessToken should succeed when the refresh flow succeeds")

		assert.Equal(t, "new-access-token", token, "GetAccessToken should return the refreshed access token")

		assertStoredAccessToken(t, r.creds, "new-access-token")
	})

	t.Run("expired token with failed refresh returns ErrNotAuthenticated without falling back to device flow", func(t *testing.T) {
		ts := newAuthServer(t, failIfCalled(t, "device code"), refreshHandler(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSONErrorResponse(t, w, http.StatusBadRequest, "invalid_grant", "refresh token expired")
		}))
		defer ts.Close()

		r := newTestResolver(t, ts)

		seedStoredTokens(t, r.creds, StoredTokens{
			LastUpdate:   time.Now().UTC().Add(-2 * time.Hour),
			AccessToken:  "old-access-token",
			RefreshToken: "old-refresh-token",
		})

		token, err := r.GetAccessToken()
		require.ErrorIs(t, err, ErrNotAuthenticated, "GetAccessToken should return ErrNotAuthenticated when refresh fails, without running the device flow")

		assert.Empty(t, token, "GetAccessToken should return an empty token on failure")
	})

	t.Run("no stored credentials returns ErrNotAuthenticated without running the device flow", func(t *testing.T) {
		ts := newAuthServer(t, failIfCalled(t, "device code"), refreshHandler(t, failIfCalled(t, "refresh")))
		defer ts.Close()

		r := newTestResolver(t, ts)

		token, err := r.GetAccessToken()
		require.ErrorIs(t, err, ErrNotAuthenticated, "GetAccessToken should return ErrNotAuthenticated when no credentials are stored")

		assert.Empty(t, token, "GetAccessToken should return an empty token on failure")
	})

	t.Run("corrupt stored credentials returns ErrNotAuthenticated without running the device flow", func(t *testing.T) {
		ts := newAuthServer(t, failIfCalled(t, "device code"), refreshHandler(t, failIfCalled(t, "refresh")))
		defer ts.Close()

		r := newTestResolver(t, ts)

		require.NoError(t, r.creds.Write([]byte("not-valid-json")), "should be able to seed the mocked keyring with unparsable content")

		token, err := r.GetAccessToken()
		require.ErrorIs(t, err, ErrNotAuthenticated, "GetAccessToken should return ErrNotAuthenticated when stored credentials cannot be unmarshaled")

		assert.Empty(t, token, "GetAccessToken should return an empty token on failure")
	})

	t.Run("keyring read failure other than not-exist returns immediately without any network call", func(t *testing.T) {
		ts := newAuthServer(t, failIfCalled(t, "device code"), failIfCalled(t, "refresh"))
		defer ts.Close()

		r := newTestResolver(t, ts)

		keyring.MockInitWithError(errors.New("keyring backend unavailable"))

		token, err := r.GetAccessToken()
		require.Error(t, err, "GetAccessToken should surface a keyring read failure that is not ErrCredentialsNotExist")

		assert.Empty(t, token, "GetAccessToken should return an empty token on failure")
	})
}

func TestLogin(t *testing.T) {
	t.Run("valid cached token is a no-op: no network call, no keyring write", func(t *testing.T) {
		ts := newAuthServer(t, failIfCalled(t, "device code"), failIfCalled(t, "refresh"))
		defer ts.Close()

		r := newTestResolver(t, ts)

		seeded := StoredTokens{
			LastUpdate:   time.Now().UTC(),
			AccessToken:  "cached-access-token",
			RefreshToken: "cached-refresh-token",
			Scope:        testScope,
		}

		seedStoredTokens(t, r.creds, seeded)

		st, err := r.Login()
		require.NoError(t, err, "Login should succeed when a non-expired token is already stored")

		assert.Equal(t, seeded, st, "Login should return the cached tokens unchanged")
		assert.Equal(t, seeded, readStoredTokens(t, r.creds), "Login should not have rewritten the stored credentials")
	})

	t.Run("expired token with working refresh is refreshed and re-cached without running the device flow", func(t *testing.T) {
		ts := newAuthServer(t, failIfCalled(t, "device code"), refreshHandler(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "old-refresh-token", r.FormValue("refresh_token"), "refresh request should carry the stored refresh token")

			writeJSONTokenResponse(t, w, http.StatusOK, "new-access-token", "new-refresh-token")
		}))
		defer ts.Close()

		r := newTestResolver(t, ts)

		seedStoredTokens(t, r.creds, StoredTokens{
			LastUpdate:   time.Now().UTC().Add(-2 * time.Hour),
			AccessToken:  "old-access-token",
			RefreshToken: "old-refresh-token",
		})

		st, err := r.Login()
		require.NoError(t, err, "Login should succeed when the refresh flow succeeds")

		assert.Equal(t, "new-access-token", st.AccessToken, "Login should return the refreshed access token")
		assert.Equal(t, testScope, st.Scope, "Login should return the scope reported by the refresh flow")

		assertStoredAccessToken(t, r.creds, "new-access-token")
	})

	t.Run("expired token with failed refresh falls back to the device flow", func(t *testing.T) {
		ts := newAuthServer(t, deviceCodeHandler(t), refreshHandler(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSONErrorResponse(t, w, http.StatusBadRequest, "invalid_grant", "refresh token expired")
		}))
		defer ts.Close()

		r := newTestResolver(t, ts)

		seedStoredTokens(t, r.creds, StoredTokens{
			LastUpdate:   time.Now().UTC().Add(-2 * time.Hour),
			AccessToken:  "old-access-token",
			RefreshToken: "old-refresh-token",
		})

		st, err := r.Login()
		require.NoError(t, err, "Login should fall back to the device flow when refresh fails")

		assert.Equal(t, "device-access-token", st.AccessToken, "Login should return the token obtained from the device flow")
		assert.Equal(t, testScope, st.Scope, "Login should return the scope reported by the device flow")

		assertStoredAccessToken(t, r.creds, "device-access-token")
	})

	t.Run("no stored credentials runs the device flow and caches the resulting tokens and scope", func(t *testing.T) {
		ts := newAuthServer(t, deviceCodeHandler(t), refreshHandler(t, failIfCalled(t, "refresh")))
		defer ts.Close()

		r := newTestResolver(t, ts)

		st, err := r.Login()
		require.NoError(t, err, "Login should succeed via the device flow when no credentials are stored")

		assert.Equal(t, "device-access-token", st.AccessToken, "Login should return the token obtained from the device flow")

		stored := readStoredTokens(t, r.creds)
		assert.Equal(t, "device-access-token", stored.AccessToken, "the device flow's access token should be cached in the keyring")
		assert.Equal(t, "device-refresh-token", stored.RefreshToken, "the device flow's refresh token should be cached in the keyring")
		assert.Equal(t, testScope, stored.Scope, "the device flow's granted scope should be cached in the keyring")
	})

	t.Run("keyring read failure other than not-exist surfaces immediately without running the device flow", func(t *testing.T) {
		ts := newAuthServer(t, failIfCalled(t, "device code"), failIfCalled(t, "refresh"))
		defer ts.Close()

		r := newTestResolver(t, ts)

		keyring.MockInitWithError(errors.New("keyring backend unavailable"))

		st, err := r.Login()
		require.Error(t, err, "Login should surface a keyring read failure that is not ErrCredentialsNotExist")
		require.NotErrorIs(t, err, ErrNotAuthenticated, "an unrelated keyring failure should not be reported as ErrNotAuthenticated")

		assert.Empty(t, st, "Login should return zero-value StoredTokens on failure")
	})
}

func TestStatus(t *testing.T) {
	t.Run("valid cached token reports logged in without any network call", func(t *testing.T) {
		ts := newAuthServer(t, failIfCalled(t, "device code"), failIfCalled(t, "refresh"))
		defer ts.Close()

		r := newTestResolver(t, ts)

		seeded := StoredTokens{
			LastUpdate:   time.Now().UTC(),
			AccessToken:  "cached-access-token",
			RefreshToken: "cached-refresh-token",
			Scope:        testScope,
		}

		seedStoredTokens(t, r.creds, seeded)

		st, loggedIn, err := r.Status()
		require.NoError(t, err, "Status should not error for a non-expired cached token")

		assert.True(t, loggedIn, "Status should report loggedIn=true for a non-expired cached token")
		assert.Equal(t, seeded, st, "Status should return the cached tokens unchanged")
	})

	t.Run("expired token with working refresh refreshes, re-caches, and reports logged in", func(t *testing.T) {
		ts := newAuthServer(t, failIfCalled(t, "device code"), refreshHandler(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSONTokenResponse(t, w, http.StatusOK, "new-access-token", "new-refresh-token")
		}))
		defer ts.Close()

		r := newTestResolver(t, ts)

		seedStoredTokens(t, r.creds, StoredTokens{
			LastUpdate:   time.Now().UTC().Add(-2 * time.Hour),
			AccessToken:  "old-access-token",
			RefreshToken: "old-refresh-token",
		})

		st, loggedIn, err := r.Status()
		require.NoError(t, err, "Status should not error when the refresh flow succeeds")

		assert.True(t, loggedIn, "Status should report loggedIn=true after a successful refresh")
		assert.Equal(t, "new-access-token", st.AccessToken, "Status should return the refreshed access token")

		assertStoredAccessToken(t, r.creds, "new-access-token")
	})

	t.Run("no stored credentials reports not logged in without error and without running the device flow", func(t *testing.T) {
		ts := newAuthServer(t, failIfCalled(t, "device code"), refreshHandler(t, failIfCalled(t, "refresh")))
		defer ts.Close()

		r := newTestResolver(t, ts)

		st, loggedIn, err := r.Status()
		require.NoError(t, err, "Status should treat a cache miss as loggedIn=false, not an error")

		assert.False(t, loggedIn, "Status should report loggedIn=false when no credentials are stored")
		assert.Empty(t, st, "Status should return zero-value StoredTokens when not logged in")
	})

	t.Run("expired token with failed refresh reports not logged in without error and without running the device flow", func(t *testing.T) {
		ts := newAuthServer(t, failIfCalled(t, "device code"), refreshHandler(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSONErrorResponse(t, w, http.StatusBadRequest, "invalid_grant", "refresh token expired")
		}))
		defer ts.Close()

		r := newTestResolver(t, ts)

		seedStoredTokens(t, r.creds, StoredTokens{
			LastUpdate:   time.Now().UTC().Add(-2 * time.Hour),
			AccessToken:  "old-access-token",
			RefreshToken: "old-refresh-token",
		})

		st, loggedIn, err := r.Status()
		require.NoError(t, err, "Status should treat a failed refresh as loggedIn=false, not an error")

		assert.False(t, loggedIn, "Status should report loggedIn=false when refresh fails")
		assert.Empty(t, st, "Status should return zero-value StoredTokens when not logged in")
	})

	t.Run("keyring read failure other than not-exist surfaces as a real error", func(t *testing.T) {
		ts := newAuthServer(t, failIfCalled(t, "device code"), failIfCalled(t, "refresh"))
		defer ts.Close()

		r := newTestResolver(t, ts)

		keyring.MockInitWithError(errors.New("keyring backend unavailable"))

		st, loggedIn, err := r.Status()
		require.Error(t, err, "Status should surface a keyring read failure that is not ErrCredentialsNotExist")
		require.NotErrorIs(t, err, ErrNotAuthenticated, "an unrelated keyring failure should not be reported as loggedIn=false")

		assert.False(t, loggedIn, "Status should report loggedIn=false alongside the error")
		assert.Empty(t, st, "Status should return zero-value StoredTokens on failure")
	})
}

func newTestResolver(t *testing.T, ts *httptest.Server) RegistryTokenResolver {
	t.Helper()

	keyring.MockInit()

	scopes := []string{"registry:read"}
	deviceCodeUrl := ts.URL + "/device"
	tokenUrl := ts.URL + "/token"

	return RegistryTokenResolver{
		logger:        log.New(io.Discard, "", 0),
		creds:         creds.NewReadWriter(testService, testUser),
		deviceCodeUrl: deviceCodeUrl,
		tokenUrl:      tokenUrl,
		scopes:        scopes,
		flow: &oauth.Flow{
			Host: &oauth.Host{
				DeviceCodeURL: deviceCodeUrl,
				TokenURL:      tokenUrl,
			},
			ClientID:    clientId,
			Scopes:      scopes,
			DisplayCode: func(string, string) error { return nil },
			BrowseURL:   func(string) error { return nil },
		},
	}
}

// newAuthServer wires up a test server exposing /device and /token routes, delegating each to the given handler.
func newAuthServer(t *testing.T, deviceHandler, tokenHandler http.HandlerFunc) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /device", deviceHandler)
	mux.HandleFunc("POST /token", tokenHandler)

	return httptest.NewServer(mux)
}

// deviceCodeHandler responds with a device code payload with interval=0 so the device flow completes immediately in tests.
func deviceCodeHandler(t *testing.T) http.HandlerFunc {
	t.Helper()

	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")

		_, err := w.Write([]byte("device_code=DEVICE123&user_code=USER-CODE&verification_uri=http://example.com/verify&interval=0&expires_in=60"))
		require.NoError(t, err, "should be able to write the mocked device code response")
	}
}

// refreshHandler dispatches only the refresh_token grant to the given handler and treats the device grant
// as an immediate success, since GetAccessToken falls back to the device flow whenever refresh fails.
func refreshHandler(t *testing.T, onRefresh http.HandlerFunc) http.HandlerFunc {
	t.Helper()

	return func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm(), "token endpoint should be able to parse the request form")

		switch r.FormValue("grant_type") {
		case "refresh_token":
			onRefresh(w, r)
		case "urn:ietf:params:oauth:grant-type:device_code":
			writeJSONTokenResponse(t, w, http.StatusOK, "device-access-token", "device-refresh-token")
		default:
			t.Fatalf("unexpected grant_type %q sent to the token endpoint", r.FormValue("grant_type"))
		}
	}
}

func failIfCalled(t *testing.T, what string) http.HandlerFunc {
	t.Helper()

	return func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("%s endpoint should not have been called", what)
	}
}

func writeJSONTokenResponse(t *testing.T, w http.ResponseWriter, status int, accessToken, refreshToken string) {
	t.Helper()

	body, err := json.Marshal(map[string]string{
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"token_type":    "bearer",
		"scope":         testScope,
	})
	require.NoError(t, err, "should be able to marshal the mocked token response")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	_, err = w.Write(body)
	require.NoError(t, err, "should be able to write the mocked token response")
}

func writeJSONErrorResponse(t *testing.T, w http.ResponseWriter, status int, code, description string) {
	t.Helper()

	body, err := json.Marshal(map[string]string{
		"error":             code,
		"error_description": description,
	})
	require.NoError(t, err, "should be able to marshal the mocked error response")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	_, err = w.Write(body)
	require.NoError(t, err, "should be able to write the mocked error response")
}

func seedStoredTokens(t *testing.T, rw creds.KeyringReadWriter, st StoredTokens) {
	t.Helper()

	content, err := json.Marshal(st)
	require.NoError(t, err, "should be able to marshal the StoredTokens fixture")

	require.NoError(t, rw.Write(content), "should be able to seed the mocked keyring with the StoredTokens fixture")
}

func assertStoredAccessToken(t *testing.T, rw creds.KeyringReadWriter, wantAccessToken string) {
	t.Helper()

	st := readStoredTokens(t, rw)

	assert.Equal(t, wantAccessToken, st.AccessToken, "the persisted access token should match the one returned by GetAccessToken")
}

func readStoredTokens(t *testing.T, rw creds.KeyringReadWriter) StoredTokens {
	t.Helper()

	content, err := rw.Read()
	require.NoError(t, err, "should be able to read back the stored credentials")

	var st StoredTokens

	require.NoError(t, json.Unmarshal(content, &st), "should be able to unmarshal the stored credentials")

	return st
}

// mockUserHomeDir points os.UserHomeDir at a fresh temp directory for the
// duration of the test, so LoginCommand/StatusCommand's BeforeReset resolves
// a throwaway home directory instead of the real one. It restores the
// original HOME/USERPROFILE values on cleanup.
func mockUserHomeDir(t *testing.T) string {
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

	return mockHomeDir
}
