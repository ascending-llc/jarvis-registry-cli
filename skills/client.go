package skills

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	registryHttp "github.com/ascending-llc/jarvis-registry-cli/internal/http"
)

type (
	// Client is a Registry API client for listing and fetching skills.
	Client struct {
		scheme      string
		host        string
		basePath    string
		accessToken string
	}

	// Metadata identifies a skill and its current version.
	Metadata struct {
		Id      string `json:"id"`
		Name    string `json:"name"`
		Version int    `json:"version"`

		// FileCount is the number of supporting files the skill has
		// beyond its SKILL.md. CreatedByRegistry reports whether the
		// skill was created by Jarvis Registry itself (as opposed to
		// Jarvis Chat); together they decide whether sync-skills can read
		// the skill's supporting files (see partitionSkippableSkills).
		FileCount         int  `json:"fileCount"`
		CreatedByRegistry bool `json:"createdByRegistry"`
	}

	// ContentFile is one supporting file returned inside a
	// get-skill-content response, mirroring the subset of the API's
	// SkillFileResponse the CLI needs to write the file to disk. For an
	// available file the Registry populates exactly one of Content and
	// Body: Content holds the file's UTF-8 text, and Body holds
	// base64-encoded bytes whenever the file could not be represented as
	// text (a binary file, or one whose bytes fail to decode as UTF-8).
	// IsBinary is informational only — see stageSkillContent, which keys
	// its decode off Body's presence.
	ContentFile struct {
		RelativePath      string `json:"relativePath"`
		Content           string `json:"content"`
		Body              string `json:"body"`
		UnavailableReason string `json:"unavailableReason"`
		IsBinary          bool   `json:"isBinary"`
		IsExecutable      bool   `json:"isExecutable"`
		Available         bool   `json:"available"`
	}

	// ListResponse is the decoded response body of a list-skills request.
	ListResponse struct {
		Skills []Metadata `json:"skills"`
	}

	// Content is the decoded response body of a get-skill-content
	// request.
	Content struct { //nolint:govet // fieldalignment: field order is kept readable/logical, not packed for size. Inline, not a .golangci.yaml exclusions.rules entry, because fieldalignment's diagnostic text encodes struct-specific byte-count arithmetic and can't be pinned to a stable text: regex the way this repo's other suppressions can.
		Id          string         `json:"id"`
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Body        string         `json:"body"`
		Frontmatter map[string]any `json:"frontmatter"`

		DisableModelInvocation bool `json:"disableModelInvocation"`
		UserInvocable          bool `json:"userInvocable"`

		// AllowedTools is nullable on the real API response (list[str] |
		// None), which encoding/json preserves as the nil/non-nil
		// distinction on this field — renderSkillMarkdown relies on that to
		// tell "not specified, fall back to frontmatter" apart from an
		// explicit empty list.
		AllowedTools []string `json:"allowedTools"`

		// Files carries every supporting file beyond SKILL.md. It is empty
		// for a single-SKILL.md skill.
		Files []ContentFile `json:"files"`
	}
)

var (
	// ErrFailureStatusCode indicates the Registry responded with a
	// non-200 status code.
	ErrFailureStatusCode = errors.New("Registry request returned non-200 status code")
)

// NewClient returns a Client for the Registry at registryUrl,
// authenticating requests with token.
func NewClient(registryUrl string, token string) (Client, error) {
	registryUrl = strings.TrimSuffix(registryUrl, "/")

	u, err := url.Parse(registryUrl)
	if err != nil {
		return Client{}, fmt.Errorf("failed to parse Registry url: %s", err.Error())
	}

	return Client{
		scheme:      u.Scheme,
		host:        u.Host,
		basePath:    u.Path,
		accessToken: token,
	}, nil
}

func (c Client) newListSkillsRequest() (*http.Request, error) {
	u := url.URL{
		Scheme: c.scheme,
		Host:   c.host,
		Path:   fmt.Sprintf("%s/api/v1/skills", c.basePath),
	}

	q := url.Values{}
	q.Set("enabled", "true")

	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create list_skills request: %s", err.Error())
	}

	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", c.accessToken))

	return req, nil
}

func (c Client) newGetSkillContentRequest(skillId string) (*http.Request, error) {
	u := url.URL{
		Scheme: c.scheme,
		Host:   c.host,
		Path:   fmt.Sprintf("%s/api/v1/skills/%s/content", c.basePath, url.PathEscape(skillId)),
	}

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create get_skill_content request: %s", err.Error())
	}

	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", c.accessToken))

	return req, nil
}

func (c Client) checkStatusCode(resp *http.Response) error {
	if resp.StatusCode != http.StatusOK {
		if body, err := io.ReadAll(resp.Body); err != nil {
			return fmt.Errorf("%w: code '%d': failed to read response body: %s", ErrFailureStatusCode, resp.StatusCode, err.Error())
		} else {
			return fmt.Errorf("%w: code '%d', body '%s'", ErrFailureStatusCode, resp.StatusCode, string(body))
		}
	}

	return nil
}

// ListSkills returns the metadata of every skill the caller has access to.
func (c Client) ListSkills() ([]Metadata, error) {
	req, err := c.newListSkillsRequest()
	if err != nil {
		return nil, err
	}

	resp, err := registryHttp.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make list_skills request: %s", err.Error())
	}

	defer func() { _ = resp.Body.Close() }()

	if err = c.checkStatusCode(resp); err != nil {
		return nil, err
	}

	var listResp ListResponse

	if err = json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal list_skills response: %s", err.Error())
	}

	return listResp.Skills, nil
}

// GetSkillContent returns the content of the skill identified by skillId.
func (c Client) GetSkillContent(skillId string) (Content, error) {
	req, err := c.newGetSkillContentRequest(skillId)
	if err != nil {
		return Content{}, err
	}

	resp, err := registryHttp.DefaultClient.Do(req)
	if err != nil {
		return Content{}, fmt.Errorf("failed to make get_skill_content request: %s", err.Error())
	}

	defer func() { _ = resp.Body.Close() }()

	if err = c.checkStatusCode(resp); err != nil {
		return Content{}, err
	}

	var content Content

	if err = json.NewDecoder(resp.Body).Decode(&content); err != nil {
		return Content{}, fmt.Errorf("failed to unmarshal get_skill_content response: %s", err.Error())
	}

	return content, nil
}
