package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/cli/oauth"
	"github.com/cli/oauth/api"

	"github.com/ascending-llc/jarvis-registry-cli/creds"
	registryHttp "github.com/ascending-llc/jarvis-registry-cli/internal/http"
)

type (
	// Logger is the minimal logging interface required by this package,
	// satisfied by *log.Logger.
	Logger interface {
		Print(v ...any)
		Printf(format string, v ...any)
		Println(v ...any)
	}

	// RegistryTokenResolver resolves a Jarvis Registry access token,
	// performing an OAuth device grant when no cached token exists and
	// transparently refreshing an expired one. Resolved tokens are cached
	// in the OS keyring via creds.KeyringReadWriter.
	RegistryTokenResolver struct {
		flow          *oauth.Flow
		logger        Logger
		creds         creds.KeyringReadWriter
		deviceCodeUrl string
		tokenUrl      string
		scopes        []string
	}

	// StoredTokens is the JSON representation of the OAuth tokens cached
	// in the OS keyring.
	StoredTokens struct {
		LastUpdate   time.Time `json:"last_update,omitzero"`
		AccessToken  string    `json:"access_token"`
		RefreshToken string    `json:"refresh_token"`
	}
)

const (
	// jarvisRegistryService is the Keychain "service" namespace prefix for
	// this CLI's cached credentials. NewRegistryTokenResolver combines it
	// with the target Registry's baseUrl so tokens for different Registry
	// deployments (e.g. a local dev server vs. a client's production
	// instance) are cached under distinct Keychain entries instead of
	// overwriting each other.
	jarvisRegistryService = "jarvis-registry"

	jarvisRegistryCli = "jarvis-registry-cli"

	deviceCodePath = "/auth/oauth2/device/code"

	tokenPath = "/auth/oauth2/token"

	clientId = "jarvis-registry-cli"

	expiration = time.Minute * 59
)

// NewRegistryTokenResolver builds a RegistryTokenResolver for the Registry
// at baseUrl, requesting the given OAuth scopes. logger receives
// diagnostic messages for non-fatal failures, such as failing to cache a
// newly obtained token.
func NewRegistryTokenResolver(baseUrl string, scopes []string, logger Logger) RegistryTokenResolver {
	baseUrl = strings.TrimSuffix(baseUrl, "/")

	r := RegistryTokenResolver{
		logger:        logger,
		creds:         creds.NewReadWriter(fmt.Sprintf("%s:%s", jarvisRegistryService, baseUrl), jarvisRegistryCli),
		deviceCodeUrl: baseUrl + deviceCodePath,
		tokenUrl:      baseUrl + tokenPath,
		scopes:        scopes,
	}

	r.flow = &oauth.Flow{
		Host: &oauth.Host{
			DeviceCodeURL: r.deviceCodeUrl,
			TokenURL:      r.tokenUrl,
		},
		ClientID:   clientId,
		Scopes:     scopes,
		HTTPClient: registryHttp.DefaultClient,
	}

	return r
}

// GetAccessToken returns a valid Registry access token. It prefers a
// cached, unexpired token; failing that, it refreshes a cached token via
// the OAuth refresh flow; and as a last resort, it performs a full OAuth
// device flow. Successfully obtained tokens are cached in the OS keyring
// for reuse.
func (r RegistryTokenResolver) GetAccessToken() (string, error) {
	var st StoredTokens

	var shouldDeviceFlow = false

	content, err := r.creds.Read()
	if err != nil && !errors.Is(err, creds.ErrCredentialsNotExist) {
		// Credentials exist in OS keyring but cannot be read.
		return "", err
	}

	if errors.Is(err, creds.ErrCredentialsNotExist) {
		// Credentials does not exist at all. Perform device flow.
		shouldDeviceFlow = true
	}

	// Stored credentials are successfully retrieved from OS keyring.
	if !shouldDeviceFlow {
		if err = json.Unmarshal(content, &st); err != nil {
			r.logger.Printf("failed to unmarshal stored credentials: %s\n", err.Error())

			shouldDeviceFlow = true
		}
	}

	// Successfully unmarshaled stored credentials
	if !shouldDeviceFlow && err == nil {
		if st.LastUpdate.Add(expiration).After(time.Now().UTC()) {
			// Access token is still valid.
			return st.AccessToken, nil
		}

		// Access token has expired. Perform refresh flow.
		if err = r.refreshFlow(st.RefreshToken, &st); err == nil {
			return st.AccessToken, nil
		}

		// Refresh flow failed. Need device flow next.
	}

	if err = r.deviceFlow(&st); err == nil {
		return st.AccessToken, nil
	}

	return "", err
}

// deviceFlow runs the OAuth device grant, populating st with the
// resulting tokens. Caching the tokens in the OS keyring is best-effort:
// a caching failure is logged, not returned, so a successful token
// exchange still succeeds even if it can't be persisted.
func (r RegistryTokenResolver) deviceFlow(st *StoredTokens) error {
	resp, err := r.flow.DeviceFlow()
	if err != nil {
		return fmt.Errorf("failed to complete OAuth device flow: %s", err.Error())
	}

	st.AccessToken = resp.Token
	st.RefreshToken = resp.RefreshToken
	st.LastUpdate = time.Now().UTC()

	content, err := json.Marshal(st)
	if err != nil {
		r.logger.Printf("failed to marshal device flow tokens: %s\n", err.Error())

		return nil
	}

	if err = r.creds.Write(content); err != nil {
		r.logger.Println(err.Error())
	}

	return nil
}

// refreshFlow exchanges refreshToken for a new access/refresh token pair
// via the OAuth refresh grant, populating st with the result. As with
// deviceFlow, caching the refreshed tokens in the OS keyring is
// best-effort: a caching failure is logged, not returned.
func (r RegistryTokenResolver) refreshFlow(refreshToken string, st *StoredTokens) error {
	values := url.Values{
		"client_id":     {clientId},
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}

	resp, err := api.PostForm(registryHttp.DefaultClient, r.tokenUrl, values)
	if err != nil {
		return fmt.Errorf("failed to finish refresh flow: %s", err.Error())
	}

	tokens, err := resp.AccessToken()
	if err != nil {
		return fmt.Errorf("failed to extract tokens from refresh flow response: %s", err.Error())
	}

	st.AccessToken = tokens.Token
	st.RefreshToken = tokens.RefreshToken
	st.LastUpdate = time.Now().UTC()

	content, err := json.Marshal(&st)
	if err != nil {
		r.logger.Printf("failed to marshal refresh flow tokens: %s\n", err.Error())

		return nil
	}

	if err = r.creds.Write(content); err != nil {
		r.logger.Println(err.Error())
	}

	return nil
}
