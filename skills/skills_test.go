package skills

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

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
		{name: "evil|skill", safe: false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.safe, isSafeSkillName(c.name), "isSafeSkillName(%q) mismatch", c.name)
		})
	}
}

func TestSyncCommandBeforeReset(t *testing.T) {
	cmd := SyncCommand{}

	err := cmd.BeforeReset()
	require.NoError(t, err, "should be able to call SyncCommand.BeforeReset without error")

	logger, ok := cmd.logger.(*log.Logger)
	require.True(t, ok, "logger should be a *log.Logger")
	assert.Empty(t, logger.Prefix(), "logger should not have a prefix, so its output isn't run together with unprefixed messages")
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

			pluginRoot := t.TempDir()

			cmd.configLoadFunc = func(string) (cfg.Config, error) {
				var config cfg.Config

				config.Registry.BaseUrl = c.baseUrl
				config.Registry.AuthBaseUrl = c.authBaseUrl
				config.Local.PluginRoot = pluginRoot
				config.Local.Dest = filepath.Join(pluginRoot, "skills")

				return config, nil
			}

			err = cmd.AfterApply()
			require.NoError(t, err, "should be able to call SyncCommand.AfterApply without error")

			assert.Equal(t, c.baseUrl, cmd.baseUrl, "baseUrl should be taken from config.Registry.BaseUrl")
			assert.Equal(t, c.wantAuthBaseUrl, cmd.authBaseUrl, "authBaseUrl should be taken from config.Registry.AuthBaseUrl, distinct from baseUrl when cfg.Load resolved it that way")
			assert.Equal(t, pluginRoot, cmd.pluginRoot, "pluginRoot should be taken from config.Local.PluginRoot")

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
		cmd, mockSkillsDir, _ := newTestSyncSetup(t, ts)

		bootstrapPluginRoot(t, cmd.pluginRoot, mockSkillsDir)

		initialManifest, err := os.ReadFile(filepath.Join("testdata", "initial-state.skill-lock.json"))
		require.NoError(t, err, "should be able to read the initial-state skill-lock.json fixture")

		err = os.WriteFile(filepath.Join(cmd.pluginRoot, manifestFileName), initialManifest, 0644)
		require.NoError(t, err, "should be able to write the initial manifest file to the mocked plugin root")

		err = os.CopyFS(mockSkillsDir, os.DirFS(filepath.Join("testdata", "initial-state")))
		require.NoError(t, err, "should be able to copy the initial-state fixture directory tree to the mocked skills directory")

		var buf bytes.Buffer

		cmd.logger = log.New(&buf, "", 0)

		err = cmd.Run()
		assertPartialFailure(t, err)

		assertSyncResult(t, mockSkillsDir, cmd.pluginRoot)

		assert.NotContains(t, buf.String(), "First time skill sync", "no banner should print when the plugin was already bootstrapped")

		rows := parseMarkdownSummaryRows(t, buf.String())

		assertSummaryRow(t, rows, "to-create-skill-7", statusCreated, "-", "7", "")
		assertSummaryRow(t, rows, "not-in-remote-and-name-collide-skill-3", statusCreated, "-", "6", "")
		assertSummaryRow(t, rows, "swap-skill-alpha", statusUpdated, "1", "2", "renamed from swap-skill-beta")
		assertSummaryRow(t, rows, "swap-skill-beta", statusUpdated, "1", "2", "renamed from swap-skill-alpha")
		assertSummaryRow(t, rows, "to-update-skill-4", statusUpdated, "3", "4", "")
		assertSummaryRow(t, rows, "accidentally-deleted-skill-5", statusUpdated, "5", "5", "renamed from accidentally-delete-skill-5")
		assertSummaryRow(t, rows, "no-update-skill-1", statusUnchanged, "1", "1", "")
		assertSummaryRow(t, rows, "not-in-remote-skill-2", statusRemoved, "2", "-", "")
		assertSummaryRow(t, rows, "not-in-remote-and-name-collide-skill-3", statusRemoved, "3", "-", "")

		failedRow := findSummaryRow(t, rows, "malformed-skill-10", statusFailed)
		assert.Equal(t, "-", failedRow[2], "a failed create's Previous Version should be '-'")
		assert.Equal(t, "-", failedRow[3], "a failed create's Current Version should be '-'")
		assert.Contains(t, failedRow[4], "resolved description is empty", "the Failed row's Notes should carry the underlying error message")

		// rows must be grouped by Status in the fixed order Created,
		// Updated, Unchanged, Removed, Failed, and sorted alphabetically by
		// Skill within each group.
		wantOrder := [][2]string{
			{"not-in-remote-and-name-collide-skill-3", statusCreated},
			{"to-create-skill-7", statusCreated},
			{"accidentally-deleted-skill-5", statusUpdated},
			{"swap-skill-alpha", statusUpdated},
			{"swap-skill-beta", statusUpdated},
			{"to-update-skill-4", statusUpdated},
			{"no-update-skill-1", statusUnchanged},
			{"not-in-remote-and-name-collide-skill-3", statusRemoved},
			{"not-in-remote-skill-2", statusRemoved},
			{"malformed-skill-10", statusFailed},
		}

		var gotOrder [][2]string

		for _, row := range rows {
			gotOrder = append(gotOrder, [2]string{row[0], row[1]})
		}

		assert.Equal(t, wantOrder, gotOrder, "summary rows should be grouped by status in Created/Updated/Unchanged/Removed/Failed order, sorted alphabetically by skill within each group")
	})

	t.Run("sync from empty initial state", func(t *testing.T) {
		cmd, mockSkillsDir, _ := newTestSyncSetup(t, ts)

		bootstrapPluginRoot(t, cmd.pluginRoot, mockSkillsDir)

		var buf bytes.Buffer

		cmd.logger = log.New(&buf, "", 0)

		err := cmd.Run()
		assertPartialFailure(t, err)

		assertSyncResult(t, mockSkillsDir, cmd.pluginRoot)

		assert.NotContains(t, buf.String(), "First time skill sync", "no banner should print when the plugin was already bootstrapped")

		rows := parseMarkdownSummaryRows(t, buf.String())

		for _, name := range []string{"no-update-skill-1", "to-update-skill-4", "accidentally-deleted-skill-5", "not-in-remote-and-name-collide-skill-3", "to-create-skill-7", "swap-skill-alpha", "swap-skill-beta"} {
			row := findSummaryRow(t, rows, name, statusCreated)
			assert.Equal(t, "-", row[2], "skill %s: a Created row's Previous Version should be '-'", name)
		}

		failedRow := findSummaryRow(t, rows, "malformed-skill-10", statusFailed)
		assert.Contains(t, failedRow[4], "resolved description is empty", "the Failed row's Notes should carry the underlying error message")
	})
}

// parseMarkdownSummaryRows parses the data rows of the Markdown summary
// table printed by SyncCommand.Run — skipping its header and separator
// lines — into [skill, status, previous, current, notes] cells, trimming
// the padding tablewriter adds around each cell.
func parseMarkdownSummaryRows(t *testing.T, output string) [][]string {
	t.Helper()

	var rows [][]string

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)

		if !strings.HasPrefix(line, "|") {
			continue
		}

		if strings.Trim(line, "|:- ") == "" {
			continue
		}

		cells := splitMarkdownRow(line)

		for i, c := range cells {
			cells[i] = strings.TrimSpace(c)
		}

		rows = append(rows, cells)
	}

	require.NotEmpty(t, rows, "the summary table should have rendered at least a header row")

	// rows[0] is always the header row (Skill, Status, Previous Version,
	// Current Version, Notes); callers only care about data rows.
	return rows[1:]
}

// splitMarkdownRow splits a Markdown table row line into its cell
// substrings, mirroring how a Markdown table renderer/parser treats an
// escaped pipe: "\|" is a literal pipe character within a cell, not a
// column separator, so it is un-escaped into the cell's content rather
// than being split on.
func splitMarkdownRow(line string) []string {
	runes := []rune(strings.Trim(line, "|"))

	var (
		cells []string
		b     strings.Builder
	)

	for i := 0; i < len(runes); i++ {
		switch {
		case runes[i] == '\\' && i+1 < len(runes) && runes[i+1] == '|':
			b.WriteRune('|')

			i++
		case runes[i] == '|':
			cells = append(cells, b.String())
			b.Reset()
		default:
			b.WriteRune(runes[i])
		}
	}

	cells = append(cells, b.String())

	return cells
}

// findSummaryRow returns the first parsed summary row matching skill and
// status, failing the test if none is found.
func findSummaryRow(t *testing.T, rows [][]string, skill, status string) []string {
	t.Helper()

	for _, row := range rows {
		if row[0] == skill && row[1] == status {
			return row
		}
	}

	t.Fatalf("expected a %s row for skill %q, got rows: %v", status, skill, rows)

	return nil
}

// assertSummaryRow asserts that rows contains exactly one row for skill
// under status with the given Previous Version, Current Version, and
// Notes.
func assertSummaryRow(t *testing.T, rows [][]string, skill, status, previous, current, notes string) {
	t.Helper()

	row := findSummaryRow(t, rows, skill, status)

	assert.Equal(t, previous, row[2], "skill %s: unexpected Previous Version", skill)
	assert.Equal(t, current, row[3], "skill %s: unexpected Current Version", skill)
	assert.Equal(t, notes, row[4], "skill %s: unexpected Notes", skill)
}

// newSingleSkillTestServer returns a mocked Registry server whose
// list_skills response always names exactly one skill (id, name,
// remoteVersion), and whose get_skill_content response for that id
// either succeeds with content (failContent false) or responds with a
// forced 500 (failContent true). The returned pointer reports whether
// get_skill_content was ever requested.
func newSingleSkillTestServer(t *testing.T, id, name string, remoteVersion int, content Content, failContent bool) (ts *httptest.Server, contentRequested *bool) {
	t.Helper()

	requested := false

	mux := http.NewServeMux()

	mux.Handle(fmt.Sprintf("GET %s/api/v1/skills", registryBasePath), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := json.Marshal(ListResponse{Skills: []Metadata{{Id: id, Name: name, Version: remoteVersion}}})
		require.NoError(t, err, "should be able to marshal the mock list_skills response")

		_, err = w.Write(body)
		require.NoError(t, err, "should be able to return the mock list_skills response")
	}))

	mux.Handle(fmt.Sprintf("GET %s/api/v1/skills/{skillId}/content", registryBasePath), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = true

		if failContent {
			w.WriteHeader(http.StatusInternalServerError)

			_, err := w.Write([]byte("forced get_skill_content failure"))
			require.NoError(t, err, "should be able to write the forced failure response body")

			return
		}

		content.Id = id
		content.Name = name

		body, err := json.Marshal(content)
		require.NoError(t, err, "should be able to marshal the mock get_skill_content response")

		_, err = w.Write(body)
		require.NoError(t, err, "should be able to return the mock get_skill_content response")
	}))

	ts = httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	return ts, &requested
}

// writeSingleSkillManifest writes a manifest file recording exactly one
// skill (id, name, localVersion), with a valid CLI-owned managedBy
// marker so ensurePluginRootConsent proceeds silently, to pluginRoot,
// creating pluginRoot first if necessary.
func writeSingleSkillManifest(t *testing.T, pluginRoot, id, name string, localVersion int) {
	t.Helper()

	manifest := ManifestV1{
		SchemaVersion:     manifestSchemaVersion,
		Description:       manifestDescription,
		ManagedBy:         managedByValue,
		SyncSkillsVersion: syncSkillsVersion,
		Skills:            []Metadata{{Id: id, Name: name, Version: localVersion}},
	}

	body, err := json.Marshal(manifest)
	require.NoError(t, err, "should be able to marshal the single-skill manifest fixture")

	err = os.MkdirAll(pluginRoot, 0755)
	require.NoError(t, err, "should be able to create the mocked plugin root")

	err = os.WriteFile(filepath.Join(pluginRoot, manifestFileName), body, 0444)
	require.NoError(t, err, "should be able to write the single-skill manifest fixture")
}

// bootstrapPluginRoot seeds pluginRoot and destDir as though a prior
// sync-skills run had already bootstrapped them: a plugin.json matching
// the CLI's current embedded content (so Run's first-time banner does
// not fire), and a minimal skill-lock.json carrying the CLI's managedBy
// marker and current syncSkillsVersion, with no skills recorded, so
// ensurePluginRootConsent proceeds silently. Callers that need specific
// local skills recorded overwrite the manifest file afterward — as long
// as their fixture also carries the managedBy marker.
func bootstrapPluginRoot(t *testing.T, pluginRoot, destDir string) {
	t.Helper()

	pluginManifestDir := filepath.Join(pluginRoot, ".claude-plugin")

	err := os.MkdirAll(pluginManifestDir, 0755)
	require.NoError(t, err, "should be able to create the mocked .claude-plugin directory")

	err = os.WriteFile(filepath.Join(pluginManifestDir, "plugin.json"), pluginManifestContent, 0644)
	require.NoError(t, err, "should be able to write the mocked plugin.json")

	err = os.MkdirAll(destDir, 0755)
	require.NoError(t, err, "should be able to create the mocked skills directory")

	manifest := ManifestV1{
		SchemaVersion:     manifestSchemaVersion,
		Description:       manifestDescription,
		ManagedBy:         managedByValue,
		SyncSkillsVersion: syncSkillsVersion,
	}

	body, err := json.Marshal(manifest)
	require.NoError(t, err, "should be able to marshal the mocked bootstrap manifest")

	err = os.WriteFile(filepath.Join(pluginRoot, manifestFileName), body, 0644)
	require.NoError(t, err, "should be able to write the mocked bootstrap manifest")
}

// TestSyncCommandRunFirstTimeBanner covers the first-time-sync banner:
// present, followed by a blank line and the summary table, only on the
// run where .claude-plugin/plugin.json did not already exist; absent on
// every later run.
func TestSyncCommandRunFirstTimeBanner(t *testing.T) {
	ts, _ := newSingleSkillTestServer(t, "banner-1", "test-skill", 1, Content{Description: "a test skill", Body: "Some body.\n"}, false)

	cmd, _, _ := newTestSyncSetup(t, ts)

	var buf bytes.Buffer

	cmd.logger = log.New(&buf, "", 0)

	err := cmd.Run()
	require.NoError(t, err, "Run should succeed bootstrapping the plugin root for the first time")

	lines := strings.Split(buf.String(), "\n")
	require.GreaterOrEqual(t, len(lines), 3, "output should contain at least the banner sentence, a blank line, and the table header")
	assert.Equal(t, fmt.Sprintf("First time skill sync. The %s plugin is created.", cmd.pluginRoot), lines[0], "the first line should be the first-time banner")
	assert.Empty(t, lines[1], "a blank line should follow the banner")
	assert.Contains(t, lines[2], "Skill", "the summary table header should follow the blank line")

	buf.Reset()

	err = cmd.Run()
	require.NoError(t, err, "Run should succeed again now that the plugin root exists")

	assert.NotContains(t, buf.String(), "First time skill sync", "the banner should not print once plugin.json already exists and matches")
}

// TestSyncCommandRunSyncSkillsWrapperStableAcrossRuns is a risk
// mitigation test (see cc-plugin-integration.md's Risk section): running
// Run twice in a row must leave skills/sync-skills/SKILL.md present and
// byte-identical to the CLI's embedded content after both runs — proving
// cleanDestDir's reserved-name exemption and the reconcile-before-clean
// ordering in Run never let the wrapper be deleted.
func TestSyncCommandRunSyncSkillsWrapperStableAcrossRuns(t *testing.T) {
	ts, _ := newSingleSkillTestServer(t, "wrapper-stable-1", "test-skill", 1, Content{Description: "a test skill", Body: "Some body.\n"}, false)

	cmd, mockSkillsDir, _ := newTestSyncSetup(t, ts)

	wrapperPath := filepath.Join(mockSkillsDir, reservedSyncSkillsName, "SKILL.md")

	err := cmd.Run()
	require.NoError(t, err, "the first Run should succeed")

	firstContent, err := os.ReadFile(wrapperPath)
	require.NoError(t, err, "the sync-skills wrapper should exist after the first Run")
	assert.Equal(t, string(syncSkillsSkillContent), string(firstContent), "the wrapper's content should match the CLI's embedded content after the first Run")

	err = cmd.Run()
	require.NoError(t, err, "the second Run should succeed")

	secondContent, err := os.ReadFile(wrapperPath)
	require.NoError(t, err, "the sync-skills wrapper should still exist after the second Run")
	assert.Equal(t, string(syncSkillsSkillContent), string(secondContent), "the wrapper's content should still match the CLI's embedded content after the second Run")
}

// TestSyncCommandRunRewritesStaleSyncSkillsWrapper is the other risk
// mitigation test: a skill-lock.json recording a stale syncSkillsVersion
// must cause the wrapper to be rewritten to the current embedded
// content, without ever being observed missing (cleanDestDir's exemption
// runs before the rewrite, not after a delete).
func TestSyncCommandRunRewritesStaleSyncSkillsWrapper(t *testing.T) {
	ts, _ := newSingleSkillTestServer(t, "wrapper-stale-1", "test-skill", 1, Content{Description: "a test skill", Body: "Some body.\n"}, false)

	cmd, mockSkillsDir, _ := newTestSyncSetup(t, ts)

	bootstrapPluginRoot(t, cmd.pluginRoot, mockSkillsDir)

	wrapperDir := filepath.Join(mockSkillsDir, reservedSyncSkillsName)
	wrapperPath := filepath.Join(wrapperDir, "SKILL.md")

	const staleContent = "stale wrapper content from an older CLI version"

	require.NoError(t, os.MkdirAll(wrapperDir, 0755), "should be able to create the mocked sync-skills wrapper directory")
	require.NoError(t, os.WriteFile(wrapperPath, []byte(staleContent), 0644), "should be able to write the stale wrapper content fixture")

	manifest := ManifestV1{SchemaVersion: manifestSchemaVersion, Description: manifestDescription, ManagedBy: managedByValue, SyncSkillsVersion: syncSkillsVersion - 1}

	body, err := json.Marshal(manifest)
	require.NoError(t, err, "should be able to marshal the stale-version manifest fixture")

	require.NoError(t, os.WriteFile(filepath.Join(cmd.pluginRoot, manifestFileName), body, 0644), "should be able to write the stale-version manifest fixture")

	err = cmd.Run()
	require.NoError(t, err, "Run should succeed rewriting the stale wrapper")

	rewritten, err := os.ReadFile(wrapperPath)
	require.NoError(t, err, "the sync-skills wrapper should still exist after Run rewrites it")
	assert.Equal(t, string(syncSkillsSkillContent), string(rewritten), "the stale wrapper content should have been rewritten to the CLI's current embedded content")

	actualManifestBytes, err := os.ReadFile(filepath.Join(cmd.pluginRoot, manifestFileName))
	require.NoError(t, err, "should be able to read skill-lock.json after Run")

	var actualManifest ManifestV1

	require.NoError(t, json.Unmarshal(actualManifestBytes, &actualManifest), "should be able to unmarshal skill-lock.json after Run")
	assert.Equal(t, syncSkillsVersion, actualManifest.SyncSkillsVersion, "skill-lock.json should record the current syncSkillsVersion after Run rewrites the wrapper")
}

// TestSyncCommandRunLockContention covers the advisory lock end-to-end
// through Run itself: a second Run against the same plugin root while
// the first is still in flight must fail fast with an actionable
// message, rather than race the first Run's filesystem writes.
func TestSyncCommandRunLockContention(t *testing.T) {
	listReached := make(chan struct{})
	release := make(chan struct{})

	var closeOnce sync.Once

	mux := http.NewServeMux()

	mux.Handle(fmt.Sprintf("GET %s/api/v1/skills", registryBasePath), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		closeOnce.Do(func() { close(listReached) })

		<-release

		_, err := w.Write([]byte(`{"skills":[]}`))
		require.NoError(t, err, "should be able to return the mock empty list_skills response")
	}))

	ts := httptest.NewServer(mux)
	defer ts.Close()

	cmd, _, _ := newTestSyncSetup(t, ts)

	cmd2 := cmd

	errCh := make(chan error, 1)

	go func() { errCh <- cmd.Run() }()

	<-listReached

	err := cmd2.Run()
	require.Error(t, err, "a concurrent Run against the same plugin root should fail while the first Run is in flight")
	assert.Contains(t, err.Error(), "already in progress", "the error should explain that a sync is already in progress")

	close(release)

	require.NoError(t, <-errCh, "the first Run should still succeed once unblocked")
}

// TestSyncCommandRunRecreatesMissingFolder covers the case where a
// skill's recorded LocalVersion still matches RemoteVersion under the
// same name, but its local folder was manually deleted between runs:
// needChange must still force a refresh, and the summary must report it
// as Updated with the recreated note rather than Unchanged.
func TestSyncCommandRunRecreatesMissingFolder(t *testing.T) {
	content := Content{Description: "a test skill", Body: "# Test Skill\n\nSome fetched content.\n"}

	ts, contentRequested := newSingleSkillTestServer(t, "recreate-1", "test-skill", 1, content, false)

	cmd, mockSkillsDir, _ := newTestSyncSetup(t, ts)

	writeSingleSkillManifest(t, cmd.pluginRoot, "recreate-1", "test-skill", 1)

	// mockSkillsDir/test-skill is deliberately never created, simulating a
	// folder deleted by hand between runs.

	var buf bytes.Buffer

	cmd.logger = log.New(&buf, "", 0)

	err := cmd.Run()
	require.NoError(t, err, "Run should succeed recreating the missing folder")

	assert.True(t, *contentRequested, "get_skill_content should be requested to rebuild the missing folder")

	rendered, err := os.ReadFile(filepath.Join(mockSkillsDir, "test-skill", "SKILL.md"))
	require.NoError(t, err, "the missing skill folder should have been recreated on disk")
	assert.Contains(t, string(rendered), "Some fetched content.", "the recreated SKILL.md should contain the fetched body")

	rows := parseMarkdownSummaryRows(t, buf.String())
	assertSummaryRow(t, rows, "test-skill", statusUpdated, "1", "1", recreatedNote)
}

// TestSyncCommandRunLeavesUnchangedSkillUntouched covers the converse,
// explicitly out-of-scope case: a local folder whose contents were
// hand-edited while its name and recorded version stayed put must be
// reported Unchanged and left completely untouched — stageOne only
// stats the folder, it never diffs file contents.
func TestSyncCommandRunLeavesUnchangedSkillUntouched(t *testing.T) {
	// failContent: true makes the test fail loudly if stageOne ever calls
	// get_skill_content for this unchanged skill.
	ts, contentRequested := newSingleSkillTestServer(t, "unchanged-1", "test-skill", 1, Content{}, true)

	cmd, mockSkillsDir, _ := newTestSyncSetup(t, ts)

	writeSingleSkillManifest(t, cmd.pluginRoot, "unchanged-1", "test-skill", 1)

	err := os.MkdirAll(filepath.Join(mockSkillsDir, "test-skill"), 0755)
	require.NoError(t, err, "should be able to create the pre-existing skill folder")

	const corrupted = "corrupted content that does not match what the Registry would render"

	err = os.WriteFile(filepath.Join(mockSkillsDir, "test-skill", "SKILL.md"), []byte(corrupted), 0644)
	require.NoError(t, err, "should be able to write the hand-edited SKILL.md fixture")

	var buf bytes.Buffer

	cmd.logger = log.New(&buf, "", 0)

	err = cmd.Run()
	require.NoError(t, err, "Run should succeed leaving the unchanged skill untouched")

	assert.False(t, *contentRequested, "get_skill_content should never be requested for a skill whose recorded name/version already match remote")

	actual, err := os.ReadFile(filepath.Join(mockSkillsDir, "test-skill", "SKILL.md"))
	require.NoError(t, err, "the skill folder should still be present")
	assert.Equal(t, corrupted, string(actual), "content drift under an unchanged name/version must not be detected or touched")

	rows := parseMarkdownSummaryRows(t, buf.String())
	assertSummaryRow(t, rows, "test-skill", statusUnchanged, "1", "1", "")
}

// TestSyncCommandRunFailedUpdateLeavesOldFolderIntact covers a forced
// mid-update failure: stageOne's reordered delete-after-stage sequence
// means a failure before the (now-final) atomicRemoveAll step must leave
// the pre-existing LocalName folder completely intact.
func TestSyncCommandRunFailedUpdateLeavesOldFolderIntact(t *testing.T) {
	ts, _ := newSingleSkillTestServer(t, "fail-1", "test-skill", 2, Content{}, true)

	cmd, mockSkillsDir, _ := newTestSyncSetup(t, ts)

	writeSingleSkillManifest(t, cmd.pluginRoot, "fail-1", "test-skill", 1)

	err := os.MkdirAll(filepath.Join(mockSkillsDir, "test-skill"), 0755)
	require.NoError(t, err, "should be able to create the pre-existing skill folder")

	const original = "original content that must survive a failed update"

	err = os.WriteFile(filepath.Join(mockSkillsDir, "test-skill", "SKILL.md"), []byte(original), 0644)
	require.NoError(t, err, "should be able to write the pre-existing SKILL.md fixture")

	var buf bytes.Buffer

	cmd.logger = log.New(&buf, "", 0)

	err = cmd.Run()
	require.Error(t, err, "Run should surface the forced content-fetch failure")
	assert.Contains(t, err.Error(), "test-skill", "the returned error should name the failed skill")

	actual, err := os.ReadFile(filepath.Join(mockSkillsDir, "test-skill", "SKILL.md"))
	require.NoError(t, err, "the old skill folder must still be present after a failed update")
	assert.Equal(t, original, string(actual), "a failed update must leave the pre-existing folder completely untouched")

	rows := parseMarkdownSummaryRows(t, buf.String())
	failedRow := findSummaryRow(t, rows, "test-skill", statusFailed)
	assert.Equal(t, "1", failedRow[2], "a failed update's Previous Version should be the recorded LocalVersion")
	assert.Equal(t, "1", failedRow[3], "a failed update's Current Version should reflect that the old folder is still present on disk")
	assert.Contains(t, failedRow[4], "forced get_skill_content failure", "the Failed row's Notes should carry the underlying error message")
}

// TestSyncCommandRunFailedUpdateOnAlreadyMissingFolder covers a spec
// whose local folder was already missing before the run started, whose
// stageOne call then also fails: the Failed row's Current Version must
// be "-", not a stale spec.LocalVersion, since nothing is actually
// present on disk.
func TestSyncCommandRunFailedUpdateOnAlreadyMissingFolder(t *testing.T) {
	ts, _ := newSingleSkillTestServer(t, "fail-missing-1", "test-skill", 1, Content{}, true)

	cmd, mockSkillsDir, _ := newTestSyncSetup(t, ts)

	writeSingleSkillManifest(t, cmd.pluginRoot, "fail-missing-1", "test-skill", 1)

	// mockSkillsDir/test-skill is deliberately never created.

	var buf bytes.Buffer

	cmd.logger = log.New(&buf, "", 0)

	err := cmd.Run()
	require.Error(t, err, "Run should surface the forced content-fetch failure")
	assert.Contains(t, err.Error(), "test-skill", "the returned error should name the failed skill")

	_, statErr := os.Stat(filepath.Join(mockSkillsDir, "test-skill"))
	assert.True(t, os.IsNotExist(statErr), "the skill folder should still be absent after a failed recreate attempt")

	rows := parseMarkdownSummaryRows(t, buf.String())
	failedRow := findSummaryRow(t, rows, "test-skill", statusFailed)
	assert.Equal(t, "1", failedRow[2], "a failed update's Previous Version should be the recorded LocalVersion")
	assert.Equal(t, "-", failedRow[3], "a failed update's Current Version should be '-' since nothing is present on disk")
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

	assertOnlyReservedSkillFolderExists(t, mockSkillsDir)
}

// TestSyncCommandRunRejectsSkillNameWithPipe covers the other half of
// isSafeSkillName's rejection set: a "|" in a skill name is filesystem-safe
// but would corrupt the Markdown sync summary table's column structure
// (tablewriter's Markdown renderer does not escape cell content), so Run
// must reject it up front, the same way it rejects a path-traversal name.
func TestSyncCommandRunRejectsSkillNameWithPipe(t *testing.T) {
	mux := http.NewServeMux()

	mux.Handle(fmt.Sprintf("GET %s/api/v1/skills", registryBasePath), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := w.Write([]byte(`{"skills":[{"id":"pipe-1","name":"evil|skill","version":1}]}`))
		require.NoError(t, err, "should be able to return the malicious mock list_skills response")
	}))

	ts := httptest.NewServer(mux)
	defer ts.Close()

	cmd, mockSkillsDir, _ := newTestSyncSetup(t, ts)

	err := cmd.Run()
	require.Error(t, err, "Run should reject a remote skill name containing a pipe character")
	assert.Contains(t, err.Error(), "unsafe to use as a filesystem path", "error should explain why Run failed")

	assertOnlyReservedSkillFolderExists(t, mockSkillsDir)
}

// TestSyncCommandRunRejectsReservedSkillName covers the case where a
// Registry account has a skill named (case-insensitively) the same as
// this CLI's own reserved wrapper skill: Run must reject it up front,
// before any filesystem write, rather than let it collide with
// skills/sync-skills/.
func TestSyncCommandRunRejectsReservedSkillName(t *testing.T) {
	cases := []string{"sync-skills", "Sync-Skills", "SYNC-SKILLS"}

	for _, remoteName := range cases {
		t.Run(remoteName, func(t *testing.T) {
			mux := http.NewServeMux()

			mux.Handle(fmt.Sprintf("GET %s/api/v1/skills", registryBasePath), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := json.Marshal(ListResponse{Skills: []Metadata{{Id: "reserved-1", Name: remoteName, Version: 1}}})
				require.NoError(t, err, "should be able to marshal the mock list_skills response")

				_, err = w.Write(body)
				require.NoError(t, err, "should be able to return the mock list_skills response")
			}))

			ts := httptest.NewServer(mux)
			defer ts.Close()

			cmd, mockSkillsDir, _ := newTestSyncSetup(t, ts)

			err := cmd.Run()
			require.Error(t, err, "Run should reject a remote skill name reserved for the CLI's own wrapper skill")
			assert.Contains(t, err.Error(), "reserved for this CLI's own wrapper skill", "error should explain why Run failed")

			entries, err := os.ReadDir(filepath.Join(mockSkillsDir, reservedSyncSkillsName))
			require.NoError(t, err, "should be able to list the CLI-owned sync-skills wrapper folder after the rejected sync")
			require.Len(t, entries, 1, "the CLI-owned sync-skills wrapper folder should contain only its own SKILL.md")
			assert.Equal(t, "SKILL.md", entries[0].Name(), "the CLI-owned sync-skills wrapper folder should be untouched by the rejected sync")
		})
	}
}

// assertOnlyReservedSkillFolderExists asserts that destDir contains
// nothing but the CLI-owned reservedSyncSkillsName folder — used by
// tests that expect Run to reject a remote skill before any Registry
// skill folder is created, while still allowing for the wrapper skill
// Run reconciles earlier in the same call.
func assertOnlyReservedSkillFolderExists(t *testing.T, destDir string) {
	t.Helper()

	entries, err := os.ReadDir(destDir)
	require.NoError(t, err, "should be able to list the mocked skills directory after the rejected sync")

	var names []string

	for _, e := range entries {
		names = append(names, e.Name())
	}

	assert.Equal(t, []string{reservedSyncSkillsName}, names, "no skill folder other than the CLI-owned sync-skills wrapper should have been created for the rejected sync")
}

// TestSyncCommandRunEscapesPipeInSummaryNotes covers a Failed row whose
// Notes come from an underlying error this package does not control — here,
// a Registry error response body embedded verbatim by Client.checkStatusCode
// — that happens to contain a "|". isSafeSkillName only guards Skill cells;
// this content must instead be escaped so the printed table's column
// structure survives.
func TestSyncCommandRunEscapesPipeInSummaryNotes(t *testing.T) {
	const failureBody = "registry error: field 'name' | invalid"

	mux := http.NewServeMux()

	mux.Handle(fmt.Sprintf("GET %s/api/v1/skills", registryBasePath), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := w.Write([]byte(`{"skills":[{"id":"pipe-note-1","name":"test-skill","version":1}]}`))
		require.NoError(t, err, "should be able to return the mock list_skills response")
	}))

	mux.Handle(fmt.Sprintf("GET %s/api/v1/skills/{skillId}/content", registryBasePath), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)

		_, err := w.Write([]byte(failureBody))
		require.NoError(t, err, "should be able to write the forced failure response body")
	}))

	ts := httptest.NewServer(mux)
	defer ts.Close()

	cmd, _, _ := newTestSyncSetup(t, ts)

	writeSingleSkillManifest(t, cmd.pluginRoot, "pipe-note-1", "test-skill", 1)

	var buf bytes.Buffer

	cmd.logger = log.New(&buf, "", 0)

	err := cmd.Run()
	require.Error(t, err, "Run should surface the forced content-fetch failure")

	assert.Contains(t, buf.String(), `\|`, "the '|' in the Notes cell must be escaped in the raw table output")

	rows := parseMarkdownSummaryRows(t, buf.String())
	failedRow := findSummaryRow(t, rows, "test-skill", statusFailed)
	assert.Contains(t, failedRow[4], failureBody, "the Failed row's Notes cell must still parse back to the full, unescaped error message")
}

// newTestSyncSetup wires up a SyncCommand against a mocked Registry
// server, a mocked user home directory, and a plugin root/skills
// destination that do not exist on disk yet — exercising Run's own
// bootstrap path by default, the same as a real first-ever invocation.
// Tests that need a pre-existing installation set that up explicitly
// (see bootstrapPluginRoot and writeSingleSkillManifest).
func newTestSyncSetup(t *testing.T, ts *httptest.Server) (cmd SyncCommand, mockSkillsDir, registryDir string) {
	t.Helper()

	mockHomeDir, err := os.MkdirTemp("", "mock-user-home-dir")
	require.NoError(t, err, "should be able to create temp directory as the mocked user home directory")

	mockPluginRoot, err := os.MkdirTemp("", "mock-plugin-root")
	require.NoError(t, err, "should be able to create temp directory as the mocked plugin root")

	require.NoError(t, os.RemoveAll(mockPluginRoot), "should be able to remove the mocked plugin root so Run can bootstrap it from scratch")

	mockSkillsDir = filepath.Join(mockPluginRoot, "skills")

	originalHome := os.Getenv("HOME")
	originalUserProfile := os.Getenv("USERPROFILE")

	err = os.Setenv("HOME", mockHomeDir)
	require.NoError(t, err, "should be able to set HOME env var so os.UserHomeDir returns the mocked registry directory")

	err = os.Setenv("USERPROFILE", mockHomeDir)
	require.NoError(t, err, "should be able to set USERPROFILE env var so os.UserHomeDir returns the mocked registry directory on Windows")

	t.Cleanup(func() {
		_ = os.RemoveAll(mockHomeDir)

		_ = os.RemoveAll(mockPluginRoot)

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

		config.Local.PluginRoot = mockPluginRoot

		config.Local.Dest = mockSkillsDir

		return config, nil
	}

	err = cmd.AfterApply()
	require.NoError(t, err, "should be able to call SyncCommand.AfterApply without error")

	registryDir = filepath.Join(mockHomeDir, cfg.RegistryDirName)

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

func assertSyncResult(t *testing.T, mockSkillsDir, pluginRoot string) {
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

	// the CLI-owned sync-skills wrapper is always present alongside
	// whatever Registry skills were synced, and is never tracked in
	// skills/testdata/server-response.
	expectedSkillNames = append(expectedSkillNames, reservedSyncSkillsName)

	assert.Len(t, mockSkillsEntries, len(expectedSkillNames), "mockSkillsDir should contain exactly as many entries as skills/testdata/server-response has subfolders, plus the CLI-owned sync-skills wrapper")

	var actualSkillNames []string

	for _, e := range mockSkillsEntries {
		actualSkillNames = append(actualSkillNames, e.Name())
	}

	assert.ElementsMatch(t, expectedSkillNames, actualSkillNames, "mockSkillsDir should contain folders with the same names as the subfolders of skills/testdata/server-response, plus sync-skills")

	for _, name := range expectedSkillNames {
		if name == reservedSyncSkillsName {
			continue
		}

		assertSkillFolderMatches(t, filepath.Join(serverResponseDir, name), filepath.Join(mockSkillsDir, name))
	}

	wrapperContent, err := os.ReadFile(filepath.Join(mockSkillsDir, reservedSyncSkillsName, "SKILL.md"))
	require.NoError(t, err, "the sync-skills wrapper's SKILL.md should exist after sync")
	assert.Equal(t, string(syncSkillsSkillContent), string(wrapperContent), "the sync-skills wrapper's SKILL.md should match the CLI's embedded content")

	actualManifestBytes, err := os.ReadFile(filepath.Join(pluginRoot, manifestFileName))
	require.NoError(t, err, "should be able to read the skill-lock.json file after sync")

	var actualManifest ManifestV1

	err = json.Unmarshal(actualManifestBytes, &actualManifest)
	require.NoError(t, err, "should be able to unmarshal the skill-lock.json file after sync")

	assert.Equal(t, managedByValue, actualManifest.ManagedBy, "skill-lock.json should record managedBy after a successful sync")
	assert.Equal(t, syncSkillsVersion, actualManifest.SyncSkillsVersion, "skill-lock.json should record the current syncSkillsVersion after a successful sync")
	assert.False(t, actualManifest.LastSyncedAt.IsZero(), "skill-lock.json should record a non-zero lastSyncedAt after a successful sync")

	// zeroed out before the fixture comparison below, since these three
	// fields are asserted individually above and final-state.skill-lock.json
	// carries their zero values.
	actualManifest.ManagedBy = ""
	actualManifest.SyncSkillsVersion = 0
	actualManifest.LastSyncedAt = time.Time{}

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
