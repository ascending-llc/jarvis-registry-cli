package skills

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSyncCommandRunIsolatesPartialSupportingFileFailure exercises failures
// after staging has already written SKILL.md and a supporting file. Both an
// in-place update and a renamed update must retain the old folder, publish
// no partial content, and allow an unrelated skill to be created and recorded.
func TestSyncCommandRunIsolatesPartialSupportingFileFailure(t *testing.T) {
	for _, remoteName := range []string{"old-skill", "renamed-skill"} {
		for _, failure := range []struct {
			name      string
			wantError string
			file      ContentFile
		}{
			{name: "invalid base64", file: ContentFile{RelativePath: "bad.bin", Body: "!!!", Available: true}, wantError: "failed to decode supporting file"},
			{name: "write failure", file: ContentFile{RelativePath: "good.txt/child.txt", Content: "child", Available: true}, wantError: "failed to stage supporting file"},
			{name: "unavailable file", file: ContentFile{RelativePath: "missing.bin", IsBinary: true, Available: false, UnavailableReason: "file content exceeds the per-file size cap"}, wantError: "is not available"},
			{name: "unsafe path", file: ContentFile{RelativePath: "../../etc/cron.d/evil", Content: "evil", Available: true}, wantError: "unsafe relative path"},
		} {
			t.Run(remoteName+"/"+failure.name, func(t *testing.T) {
				content := Content{Description: "updated skill", Body: "new body", Files: []ContentFile{
					{RelativePath: "good.txt", Content: "new helper", Available: true},
					failure.file,
				}}
				mux := http.NewServeMux()
				mux.HandleFunc("GET "+registryBasePath+"/api/v1/skills", func(w http.ResponseWriter, _ *http.Request) {
					err := json.NewEncoder(w).Encode(ListResponse{Skills: []Metadata{
						{Id: "old", Name: remoteName, Version: 2, FileCount: 2, CreatedByRegistry: true},
						{Id: "new", Name: "new-skill", Version: 1},
					}})
					assert.NoError(t, err, "the skill list should be returned")
				})
				mux.HandleFunc("GET "+registryBasePath+"/api/v1/skills/old/content", func(w http.ResponseWriter, _ *http.Request) {
					assert.NoError(t, json.NewEncoder(w).Encode(content), "the failing skill content should be returned")
				})
				mux.HandleFunc("GET "+registryBasePath+"/api/v1/skills/new/content", func(w http.ResponseWriter, _ *http.Request) {
					assert.NoError(t, json.NewEncoder(w).Encode(Content{Description: "new skill", Body: "complete body"}), "the new skill content should be returned")
				})
				ts := httptest.NewServer(mux)
				t.Cleanup(ts.Close)
				cmd, dest, _ := newTestSyncSetup(t, ts)
				writeSingleSkillManifest(t, cmd.pluginRoot, "old", "old-skill", 1)

				oldDir := filepath.Join(dest, "old-skill")
				require.NoError(t, os.MkdirAll(oldDir, 0755), "the old skill folder should be created")

				for _, name := range []string{"SKILL.md", "good.txt"} {
					require.NoError(t, os.WriteFile(filepath.Join(oldDir, name), []byte("original"), 0644), "the old file should be created")
				}

				var output bytes.Buffer

				cmd.logger = log.New(&output, "", 0)
				err := cmd.Run()
				require.ErrorContains(t, err, failure.wantError, "the original per-skill failure should be returned")

				for _, name := range []string{"SKILL.md", "good.txt"} {
					actual, readErr := os.ReadFile(filepath.Join(oldDir, name))
					require.NoError(t, readErr, "the original file should survive")
					assert.Equal(t, "original", string(actual), "failed staging must not modify old files")
				}

				if remoteName != "old-skill" {
					assert.NoDirExists(t, filepath.Join(dest, remoteName), "a failed renamed update must not publish a partial folder")
				}

				assert.NoDirExists(t, cmd.tempDir, "temporary content should be removed after Run")
				assert.FileExists(t, filepath.Join(dest, "new-skill", "SKILL.md"), "an unrelated create should complete")

				manifest, readErr := NewManifestReadWriter(cmd.pluginRoot).ReadManifest()
				require.NoError(t, readErr, "the updated manifest should be readable")
				assert.Equal(t, []ManifestSkill{{Id: "new", Name: "new-skill", Version: 1}}, manifest.Skills, "the successful create should be recorded without the failed version")
				rows := parseMarkdownSummaryRows(t, output.String())
				failed := findSummaryRow(t, rows, "old-skill", statusFailed)
				assert.Contains(t, failed[4], failure.wantError, "the failed row should explain the original failure")
				assertSummaryRow(t, rows, "new-skill", statusCreated, "-", "1", "")
			})
		}
	}
}
