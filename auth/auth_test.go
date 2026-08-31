package auth

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
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

	t.Run("expired token with failed refresh falls back to device flow", func(t *testing.T) {
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

		token, err := r.GetAccessToken()
		require.NoError(t, err, "GetAccessToken should fall back to the device flow when refresh fails")

		assert.Equal(t, "device-access-token", token, "GetAccessToken should return the token obtained from the device flow")

		assertStoredAccessToken(t, r.creds, "device-access-token")
	})

	t.Run("no stored credentials goes straight to device flow", func(t *testing.T) {
		ts := newAuthServer(t, deviceCodeHandler(t), refreshHandler(t, failIfCalled(t, "refresh")))
		defer ts.Close()

		r := newTestResolver(t, ts)

		token, err := r.GetAccessToken()
		require.NoError(t, err, "GetAccessToken should succeed via the device flow when no credentials are stored")

		assert.Equal(t, "device-access-token", token, "GetAccessToken should return the token obtained from the device flow")
	})

	t.Run("corrupt stored credentials falls back to device flow", func(t *testing.T) {
		ts := newAuthServer(t, deviceCodeHandler(t), refreshHandler(t, failIfCalled(t, "refresh")))
		defer ts.Close()

		r := newTestResolver(t, ts)

		require.NoError(t, r.creds.Write([]byte("not-valid-json")), "should be able to seed the mocked keyring with unparsable content")

		token, err := r.GetAccessToken()
		require.NoError(t, err, "GetAccessToken should succeed via the device flow when stored credentials cannot be unmarshaled")

		assert.Equal(t, "device-access-token", token, "GetAccessToken should return the token obtained from the device flow")
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

	content, err := rw.Read()
	require.NoError(t, err, "should be able to read back the stored credentials")

	var st StoredTokens

	require.NoError(t, json.Unmarshal(content, &st), "should be able to unmarshal the stored credentials")

	assert.Equal(t, wantAccessToken, st.AccessToken, "the persisted access token should match the one returned by GetAccessToken")
}
