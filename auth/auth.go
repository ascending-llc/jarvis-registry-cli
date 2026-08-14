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
	"github.com/ascending-llc/jarvis-registry-cli/internal/http"
	"github.com/ascending-llc/jarvis-registry-cli/logging"
)

type (
	RegistryTokenResolver struct {
		flow          *oauth.Flow
		logger        logging.Logger
		creds         creds.KeyringReadWriter
		deviceCodeUrl string
		tokenUrl      string
		scopes        []string
	}

	StoredTokens struct {
		LastUpdate   time.Time `json:"last_update,omitzero"`
		AccessToken  string    `json:"access_token"`
		RefreshToken string    `json:"refresh_token"`
	}
)

const (
	ascendingService = "ascendingdc.com"

	jarvisRegsitryCli = "jarvis-registry-cli"

	deviceCodePath = "/auth/oauth2/device/code"

	tokenPath = "/auth/oauth2/token"

	clientId = "jarvis-registry-cli"

	expiration = time.Minute * 59
)

func NewRegistryTokenResolver(baseUrl string, scopes []string, logger logging.Logger) RegistryTokenResolver {
	baseUrl = strings.TrimSuffix(baseUrl, "/")

	r := RegistryTokenResolver{
		logger:        logger,
		creds:         creds.NewReadWriter(ascendingService, jarvisRegsitryCli),
		deviceCodeUrl: baseUrl + deviceCodePath,
		tokenUrl:      baseUrl + tokenPath,
		scopes:        scopes,
	}

	r.flow = &oauth.Flow{
		Host: &oauth.Host{
			DeviceCodeURL: r.deviceCodeUrl,
			TokenURL:      r.tokenUrl,
		},
		ClientID: clientId,
		Scopes:   scopes,
	}

	return r
}

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

func (r RegistryTokenResolver) refreshFlow(refreshToken string, st *StoredTokens) error {
	values := url.Values{
		"client_id":     {clientId},
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}

	resp, err := api.PostForm(http.DefaultClient, r.tokenUrl, values)
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
