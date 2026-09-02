package skills

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ascending-llc/jarvis-registry-cli/cfg"
)

type (
	MockTokenProvider struct{}
)

const (
	mockBearerToken = "mock-bearer-token"
)

func (tp MockTokenProvider) GetAccessToken() (string, error) {
	return mockBearerToken, nil
}

func TestIsSafeSkillName(t *testing.T) {
	cases := []struct {
		name string
		safe bool
	}{
		{name: "my-skill", safe: true},
		{name: "my_skill.v2", safe: true},
		{name: "", safe: false},
		{name: ".", safe: false},
		{name: "..", safe: false},
		{name: "../../etc/cron.d/evil", safe: false},
		{name: "nested/path", safe: false},
		{name: `nested\path`, safe: false},
		{name: "/etc/passwd", safe: false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.safe, isSafeSkillName(c.name), "isSafeSkillName(%q) mismatch", c.name)
		})
	}
}

func TestSyncCommandAfterApply(t *testing.T) {
	cases := []struct {
		name            string
		baseUrl         string
		authBaseUrl     string
		wantAuthBaseUrl string
	}{
		{name: "distinct auth_base_url is wired through as-is", baseUrl: "https://registry.example.com", authBaseUrl: "http://localhost:8888", wantAuthBaseUrl: "http://localhost:8888"},
		{name: "auth_base_url defaulted to base_url by cfg.Load is wired through as-is", baseUrl: "https://registry.example.com", authBaseUrl: "https://registry.example.com", wantAuthBaseUrl: "https://registry.example.com"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cmd := SyncCommand{}

			err := cmd.BeforeReset()
			require.NoError(t, err, "should be able to call SyncCommand.BeforeReset without error")

			cmd.configLoadFunc = func(string) (cfg.Config, error) {
				var config cfg.Config

				config.Registry.BaseUrl = c.baseUrl
				config.Registry.AuthBaseUrl = c.authBaseUrl
				config.Local.Dest = t.TempDir()

				return config, nil
			}

			err = cmd.AfterApply()
			require.NoError(t, err, "should be able to call SyncCommand.AfterApply without error")

			assert.Equal(t, c.baseUrl, cmd.baseUrl, "baseUrl should be taken from config.Registry.BaseUrl")
			assert.Equal(t, c.wantAuthBaseUrl, cmd.authBaseUrl, "authBaseUrl should be taken from config.Registry.AuthBaseUrl, distinct from baseUrl when cfg.Load resolved it that way")

			require.NotNil(t, cmd.tp, "tp should be initialized")
		})
	}
}

func TestSyncCommandRun(t *testing.T) {
	listSkillsRespBody, err := os.ReadFile(filepath.Join("testdata", "server-response", "list.json"))
	require.NoError(t, err, "should be able to read the mocked list_skills response from a local file")

	var remoteSKills ListResponse

	err = json.Unmarshal(listSkillsRespBody, &remoteSKills)
	require.NoError(t, err, "should be able to JSON unmarshal mock list_skills response body")

	var skillContentRespMap = make(map[string][]byte)

	var content []byte

	for _, meta := range remoteSKills.Skills {
		content, err = os.ReadFile(filepath.Join("testdata", "server-response", meta.Name+".content.json"))
		require.NoError(t, err, fmt.Sprintf("should be able to read the mock get_skill_content fixture for skill %s", meta.Name))

		var apiContent Content

		err = json.Unmarshal(content, &apiContent)
		require.NoError(t, err, fmt.Sprintf("should be able to unmarshal the mock get_skill_content fixture for skill %s", meta.Name))

		apiContent.Id = meta.Id
		apiContent.Name = meta.Name

		content, err = json.Marshal(apiContent)
		require.NoError(t, err, fmt.Sprintf("should be able to serialize get_skill_content response for skill %s", meta.Name))

		skillContentRespMap[meta.Id] = content
	}

	mux := http.NewServeMux()

	mux.Handle(fmt.Sprintf("GET %s/api/v1/skills", registryBasePath), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Helper()

		require.Equal(t, fmt.Sprintf("Bearer %s", mockBearerToken), r.Header.Get("Authorization"), "should request list_skills with the mocked bearer token")

		require.Equal(t, "0", r.URL.Query().Get("fileCount"), "should request list_skills with fileCount=0")
		require.Equal(t, "true", r.URL.Query().Get("enabled"), "should request list_skills with enabled=true")

		_, err := w.Write(listSkillsRespBody)
		require.NoError(t, err, "should be able to return mock data to the list_skills request")
	}))

	mux.Handle(fmt.Sprintf("GET %s/api/v1/skills/{skillId}/content", registryBasePath), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Helper()

		skillId := r.PathValue("skillId")

		require.Equal(t, fmt.Sprintf("Bearer %s", mockBearerToken), r.Header.Get("Authorization"), fmt.Sprintf("should request get_skill_content for skill id %s with the mocked bearer token", skillId))

		body, ok := skillContentRespMap[skillId]
		require.True(t, ok, fmt.Sprintf("should have a mocked get_skill_content response for skill id %s", skillId))

		_, err := w.Write(body)
		require.NoError(t, err, fmt.Sprintf("should be able to return mock data to the get_skill_content request for skill id %s", skillId))
	}))

	ts := httptest.NewServer(mux)
	defer ts.Close()

	t.Run("sync from non-trivial initial state", func(t *testing.T) {
		cmd, mockSkillsDir, registryDir := newTestSyncSetup(t, ts)

		initialManifest, err := os.ReadFile(filepath.Join("testdata", "initial-state.skill-lock.json"))
		require.NoError(t, err, "should be able to read the initial-state skill-lock.json fixture")

		err = os.WriteFile(filepath.Join(registryDir, manifestFileName), initialManifest, 0444)
		require.NoError(t, err, "should be able to write the initial manifest file to the mocked registry directory")

		err = os.CopyFS(mockSkillsDir, os.DirFS(filepath.Join("testdata", "initial-state")))
		require.NoError(t, err, "should be able to copy the initial-state fixture directory tree to the mocked skills directory")

		err = cmd.Run()
		assertPartialFailure(t, err)

		assertSyncResult(t, mockSkillsDir, registryDir)
	})

	t.Run("sync from empty initial state", func(t *testing.T) {
		cmd, mockSkillsDir, registryDir := newTestSyncSetup(t, ts)

		err := cmd.Run()
		assertPartialFailure(t, err)

		assertSyncResult(t, mockSkillsDir, registryDir)
	})
}

// TestSyncCommandRunRejectsUnsafeSkillName is a defense-in-depth check
// against a misbehaving/compromised registry response: a skill name that
// escapes destDir via ".." must fail the whole Run loudly, before any
// filesystem call is made with it, rather than being silently cleaned by
// filepath.Join.
func TestSyncCommandRunRejectsUnsafeSkillName(t *testing.T) {
	mux := http.NewServeMux()

	mux.Handle(fmt.Sprintf("GET %s/api/v1/skills", registryBasePath), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := w.Write([]byte(`{"skills":[{"id":"evil-1","name":"../../etc/cron.d/evil","version":1}]}`))
		require.NoError(t, err, "should be able to return the malicious mock list_skills response")
	}))

	ts := httptest.NewServer(mux)
	defer ts.Close()

	cmd, mockSkillsDir, _ := newTestSyncSetup(t, ts)

	err := cmd.Run()
	require.Error(t, err, "Run should reject a remote skill name that is unsafe to use as a filesystem path")
	assert.Contains(t, err.Error(), "unsafe to use as a filesystem path", "error should explain why Run failed")

	entries, err := os.ReadDir(mockSkillsDir)
	require.NoError(t, err, "should be able to list the mocked skills directory after the rejected sync")
	assert.Empty(t, entries, "no skill folder should have been created for the unsafe skill name")
}

func newTestSyncSetup(t *testing.T, ts *httptest.Server) (cmd SyncCommand, mockSkillsDir, registryDir string) {
	t.Helper()

	mockHomeDir, err := os.MkdirTemp("", "mock-user-home-dir")
	require.NoError(t, err, "should be able to create temp directory as the mocked user home directory")

	mockSkillsDir, err = os.MkdirTemp("", "mock-skills-dir")
	require.NoError(t, err, "should be able to create temp directory as the mocked skills directory")

	originalHome := os.Getenv("HOME")
	originalUserProfile := os.Getenv("USERPROFILE")

	err = os.Setenv("HOME", mockHomeDir)
	require.NoError(t, err, "should be able to set HOME env var so os.UserHomeDir returns the mocked registry directory")

	err = os.Setenv("USERPROFILE", mockHomeDir)
	require.NoError(t, err, "should be able to set USERPROFILE env var so os.UserHomeDir returns the mocked registry directory on Windows")

	t.Cleanup(func() {
		_ = os.RemoveAll(mockHomeDir)

		_ = os.RemoveAll(mockSkillsDir)

		_ = os.Setenv("HOME", originalHome)

		_ = os.Setenv("USERPROFILE", originalUserProfile)
	})

	cmd = SyncCommand{}

	err = cmd.BeforeReset()
	require.NoError(t, err, "should be able to call SyncCommand.BeforeReset without error")

	cmd.configLoadFunc = func(string) (cfg.Config, error) {
		var config cfg.Config

		config.Registry.BaseUrl = ts.URL

		config.Registry.AuthBaseUrl = ts.URL

		config.Local.Dest = mockSkillsDir

		return config, nil
	}

	err = cmd.AfterApply()
	require.NoError(t, err, "should be able to call SyncCommand.AfterApply without error")

	registryDir = filepath.Join(mockHomeDir, registryDirName)

	err = os.MkdirAll(registryDir, 0755)
	require.NoError(t, err, "should be able to create the mocked registry directory")

	cmd.tp = MockTokenProvider{}

	return cmd, mockSkillsDir, registryDir
}

// assertPartialFailure asserts that Run returned the error expected from
// the shared test fixtures' one deliberately malformed skill
// ("malformed-skill-10", whose resolved description is empty across every
// source) — proving a single per-skill render failure surfaces on the
// command's returned error without aborting the rest of the sync.
func assertPartialFailure(t *testing.T, err error) {
	t.Helper()

	require.Error(t, err, "SyncCommand.Run should return a non-nil error because of the one malformed skill")
	assert.Contains(t, err.Error(), "malformed-skill-10", "the returned error should name the malformed skill")
	assert.Contains(t, err.Error(), "resolved description is empty", "the returned error should explain why rendering failed")
}

func assertSyncResult(t *testing.T, mockSkillsDir, registryDir string) {
	t.Helper()

	mockSkillsEntries, err := os.ReadDir(mockSkillsDir)
	require.NoError(t, err, "should be able to list the mocked skills directory after sync")

	serverResponseDir := filepath.Join("testdata", "server-response")

	serverResponseEntries, err := os.ReadDir(serverResponseDir)
	require.NoError(t, err, "should be able to list the server-response testdata directory")

	var expectedSkillNames []string

	for _, e := range serverResponseEntries {
		if e.IsDir() {
			expectedSkillNames = append(expectedSkillNames, e.Name())
		}
	}

	assert.Len(t, mockSkillsEntries, len(expectedSkillNames), "mockSkillsDir should contain exactly as many skill folders as skills/testdata/server-response has subfolders")

	var actualSkillNames []string

	for _, e := range mockSkillsEntries {
		actualSkillNames = append(actualSkillNames, e.Name())
	}

	assert.ElementsMatch(t, expectedSkillNames, actualSkillNames, "mockSkillsDir should contain folders with the same names as the subfolders of skills/testdata/server-response")

	for _, name := range expectedSkillNames {
		assertSkillFolderMatches(t, filepath.Join(serverResponseDir, name), filepath.Join(mockSkillsDir, name))
	}

	actualManifestBytes, err := os.ReadFile(filepath.Join(registryDir, manifestFileName))
	require.NoError(t, err, "should be able to read the skill-lock.json file after sync")

	var actualManifest ManifestV1

	err = json.Unmarshal(actualManifestBytes, &actualManifest)
	require.NoError(t, err, "should be able to unmarshal the skill-lock.json file after sync")

	expectedManifestBytes, err := os.ReadFile(filepath.Join("testdata", "final-state.skill-lock.json"))
	require.NoError(t, err, "should be able to read the final-state skill-lock.json fixture")

	var expectedManifest ManifestV1

	err = json.Unmarshal(expectedManifestBytes, &expectedManifest)
	require.NoError(t, err, "should be able to unmarshal the final-state skill-lock.json fixture")

	sort.Slice(actualManifest.Skills, func(i, j int) bool { return actualManifest.Skills[i].Id < actualManifest.Skills[j].Id })
	sort.Slice(expectedManifest.Skills, func(i, j int) bool { return expectedManifest.Skills[i].Id < expectedManifest.Skills[j].Id })

	assert.Equal(t, expectedManifest, actualManifest, "skill-lock.json should match the final-state fixture regardless of key order or skills array order")
}

func assertSkillFolderMatches(t *testing.T, expectedDir, actualDir string) {
	t.Helper()

	expectedFiles, err := collectRelativeFilePaths(expectedDir)
	require.NoError(t, err, fmt.Sprintf("should be able to walk expected skill folder %s", expectedDir))

	actualFiles, err := collectRelativeFilePaths(actualDir)
	require.NoError(t, err, fmt.Sprintf("should be able to walk actual skill folder %s", actualDir))

	if !assert.ElementsMatch(t, expectedFiles, actualFiles, fmt.Sprintf("%s should contain exactly the same relative file paths as %s", actualDir, expectedDir)) {
		return
	}

	for _, rel := range expectedFiles {
		expectedContent, err := os.ReadFile(filepath.Join(expectedDir, rel))
		require.NoError(t, err, fmt.Sprintf("should be able to read expected file %s", filepath.Join(expectedDir, rel)))

		actualContent, err := os.ReadFile(filepath.Join(actualDir, rel))
		require.NoError(t, err, fmt.Sprintf("should be able to read actual file %s", filepath.Join(actualDir, rel)))

		assert.Equal(t, string(expectedContent), string(actualContent), fmt.Sprintf("file %s should have matching content between %s and %s", rel, expectedDir, actualDir))
	}
}

func collectRelativeFilePaths(root string) ([]string, error) {
	var paths []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		paths = append(paths, rel)

		return nil
	})

	return paths, err
}
