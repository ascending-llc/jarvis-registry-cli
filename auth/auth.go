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
		Scope        string    `json:"scope"`
	}
)

const (
	// ScopeSkillsRead grants read access to skills.SyncCommand's
	// sync-skills operations.
	ScopeSkillsRead = "skills-read"

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

var (
	// ErrNotAuthenticated indicates no valid Registry credentials are
	// cached in the OS keyring and none could be obtained without user
	// interaction. GetAccessToken and Status return it instead of running
	// the OAuth device flow; only Login runs the device flow, via its own
	// fallback.
	ErrNotAuthenticated = errors.New("not logged in to the Registry: run `jarvis-registry auth login` to authenticate")

	// RegistryScopes is the full set of OAuth scopes this CLI ever
	// requests from the Registry auth server, across every command. The
	// device flow — the only place scope is actually negotiated — must
	// request this whole set, since a refreshed token cannot renegotiate
	// scope.
	RegistryScopes = []string{ScopeSkillsRead}
)

// NewRegistryTokenResolver builds a RegistryTokenResolver against the
// OAuth device/token endpoints at authServerBaseUrl, requesting the given
// OAuth scopes. authServerBaseUrl is usually the same origin as the
// Registry API itself, but callers may pass a different one when the two
// don't share an origin (e.g. local development). logger receives
// diagnostic messages for non-fatal failures, such as failing to cache a
// newly obtained token.
func NewRegistryTokenResolver(authServerBaseUrl string, scopes []string, logger Logger) RegistryTokenResolver {
	authServerBaseUrl = strings.TrimSuffix(authServerBaseUrl, "/")

	r := RegistryTokenResolver{
		logger:        logger,
		creds:         creds.NewReadWriter(fmt.Sprintf("%s:%s", jarvisRegistryService, authServerBaseUrl), jarvisRegistryCli),
		deviceCodeUrl: authServerBaseUrl + deviceCodePath,
		tokenUrl:      authServerBaseUrl + tokenPath,
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

// resolveCached returns the cached tokens, refreshing them via the OAuth
// refresh grant if the access token has expired. It never performs the
// OAuth device flow: a caller gets ErrNotAuthenticated when no credentials
// are cached, cached credentials are corrupt, or the refresh flow fails.
func (r RegistryTokenResolver) resolveCached() (StoredTokens, error) {
	var st StoredTokens

	content, err := r.creds.Read()
	if err != nil {
		if errors.Is(err, creds.ErrCredentialsNotExist) {
			return StoredTokens{}, ErrNotAuthenticated
		}

		return StoredTokens{}, err
	}

	if err = json.Unmarshal(content, &st); err != nil {
		return StoredTokens{}, ErrNotAuthenticated
	}

	if st.LastUpdate.Add(expiration).After(time.Now().UTC()) {
		return st, nil
	}

	if err = r.refreshFlow(st.RefreshToken, &st); err != nil {
		return StoredTokens{}, ErrNotAuthenticated
	}

	return st, nil
}

// GetAccessToken returns a valid Registry access token from the OS
// keyring, refreshing it first if it has expired. It never performs the
// OAuth device flow — callers that hit ErrNotAuthenticated should tell the
// user to run "jarvis-registry auth login".
func (r RegistryTokenResolver) GetAccessToken() (string, error) {
	st, err := r.resolveCached()
	if err != nil {
		return "", err
	}

	return st.AccessToken, nil
}

// Login ensures a valid Registry access token is cached: it leaves an
// already-valid cached token untouched, refreshes an expired one, and
// falls back to the OAuth device flow when no credentials are cached or
// the refresh flow fails. It backs the "auth login" command.
func (r RegistryTokenResolver) Login() (StoredTokens, error) {
	st, err := r.resolveCached()
	if err == nil {
		return st, nil
	}

	if !errors.Is(err, ErrNotAuthenticated) {
		return StoredTokens{}, err
	}

	if err = r.deviceFlow(&st); err != nil {
		return StoredTokens{}, err
	}

	return st, nil
}

// Status reports whether a valid Registry access token is currently
// cached, refreshing an expired one if possible. Unlike GetAccessToken, a
// cache miss is not an error: loggedIn is simply false. It never performs
// the OAuth device flow. It backs the "auth status" command.
func (r RegistryTokenResolver) Status() (st StoredTokens, loggedIn bool, err error) {
	st, err = r.resolveCached()
	if err == nil {
		return st, true, nil
	}

	if errors.Is(err, ErrNotAuthenticated) {
		return StoredTokens{}, false, nil
	}

	return StoredTokens{}, false, err
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
	st.Scope = resp.Scope
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
	st.Scope = tokens.Scope
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
