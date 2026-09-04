package skills

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManifestReadWriterWriteManifest(t *testing.T) {
	t.Run("round-trips through ReadManifest and leaves the file read-only with no temp litter", func(t *testing.T) {
		dir := t.TempDir()

		mrw := NewManifestReadWriter(dir)

		skills := []Metadata{{Id: "id-1", Name: "skill-1", Version: 1}}

		before := time.Now().UTC()

		err := mrw.WriteManifest(skills, syncSkillsVersion)
		require.NoError(t, err, "WriteManifest should succeed writing a brand-new manifest file")

		stat, err := os.Stat(filepath.Join(dir, manifestFileName))
		require.NoError(t, err, "the manifest file should exist after WriteManifest")
		assert.Equal(t, fs.FileMode(0444), stat.Mode().Perm(), "the manifest file should be read-only after WriteManifest")

		got, err := mrw.ReadManifest()
		require.NoError(t, err, "ReadManifest should be able to read back what WriteManifest wrote")
		assert.Equal(t, skills, got.Skills, "ReadManifest should round-trip the skills WriteManifest wrote")
		assert.Equal(t, managedByValue, got.ManagedBy, "WriteManifest should stamp ManagedBy with the CLI's marker value")
		assert.Equal(t, syncSkillsVersion, got.SyncSkillsVersion, "WriteManifest should stamp the syncSkillsVersion it was given")
		assert.False(t, got.LastSyncedAt.Before(before), "LastSyncedAt should be stamped at or after the call to WriteManifest")
		assert.Equal(t, time.UTC, got.LastSyncedAt.Location(), "LastSyncedAt should be stamped in UTC")

		entries, err := os.ReadDir(dir)
		require.NoError(t, err, "should be able to list the registry directory after WriteManifest")
		assert.Len(t, entries, 1, "no temp file should be left behind after a successful WriteManifest")
	})

	t.Run("a pre-existing manifest survives a simulated write failure byte-for-byte", func(t *testing.T) {
		dir := t.TempDir()

		mrw := NewManifestReadWriter(dir)

		err := mrw.WriteManifest([]Metadata{{Id: "id-1", Name: "skill-1", Version: 1}}, syncSkillsVersion)
		require.NoError(t, err, "should be able to write the initial manifest")

		originalBytes, err := os.ReadFile(filepath.Join(dir, manifestFileName))
		require.NoError(t, err, "should be able to read the initial manifest bytes")

		// Making the registry directory read-only forces WriteManifest's
		// os.CreateTemp call to fail before it ever touches mrw.path,
		// simulating a write that fails partway through.
		require.NoError(t, os.Chmod(dir, 0555), "should be able to make the registry directory read-only")

		t.Cleanup(func() { _ = os.Chmod(dir, 0755) })

		err = mrw.WriteManifest([]Metadata{{Id: "id-2", Name: "skill-2", Version: 2}}, syncSkillsVersion)
		require.Error(t, err, "WriteManifest should fail when it cannot create its temp file")

		require.NoError(t, os.Chmod(dir, 0755), "should be able to restore the registry directory's permissions")

		actualBytes, err := os.ReadFile(filepath.Join(dir, manifestFileName))
		require.NoError(t, err, "the pre-existing manifest file should still be present after the failed write")
		assert.Equal(t, originalBytes, actualBytes, "a failed write must leave the pre-existing manifest byte-for-byte unchanged")

		entries, err := os.ReadDir(dir)
		require.NoError(t, err, "should be able to list the registry directory after the failed write")
		assert.Len(t, entries, 1, "a failed write must not leave any temp file behind")
	})

	t.Run("a failed rename cleans up its temp file without disturbing the target", func(t *testing.T) {
		dir := t.TempDir()

		mrw := NewManifestReadWriter(dir)

		// Pre-creating the manifest path as a directory forces the final
		// os.Rename (a file onto an existing directory) to fail, without
		// disturbing os.CreateTemp, os.Write, or os.Chmod along the way.
		require.NoError(t, os.Mkdir(filepath.Join(dir, manifestFileName), 0755), "should be able to pre-create the manifest path as a directory")

		err := mrw.WriteManifest([]Metadata{{Id: "id-1", Name: "skill-1", Version: 1}}, syncSkillsVersion)
		require.Error(t, err, "WriteManifest should surface the rename failure")

		stat, err := os.Stat(filepath.Join(dir, manifestFileName))
		require.NoError(t, err, "the pre-existing directory at the manifest path should still be there")
		assert.True(t, stat.IsDir(), "the pre-existing directory at the manifest path must be left untouched by the failed rename")

		entries, err := os.ReadDir(dir)
		require.NoError(t, err, "should be able to list the registry directory after the failed rename")
		assert.Len(t, entries, 1, "the failed rename's temp file should have been cleaned up")
	})
}

func TestManifestReadWriterReadManifest(t *testing.T) {
	t.Run("a missing manifest file returns a zero ManifestV1 with no error", func(t *testing.T) {
		dir := t.TempDir()

		mrw := NewManifestReadWriter(dir)

		got, err := mrw.ReadManifest()
		require.NoError(t, err, "ReadManifest should not error when the manifest file does not exist")
		assert.Equal(t, ManifestV1{}, got, "ReadManifest should return a zero ManifestV1 when the file does not exist")
	})

	t.Run("malformed content is treated as absent, not an error", func(t *testing.T) {
		dir := t.TempDir()

		mrw := NewManifestReadWriter(dir)

		err := os.WriteFile(filepath.Join(dir, manifestFileName), []byte("not valid json"), 0644)
		require.NoError(t, err, "should be able to write malformed content to the manifest path")

		got, err := mrw.ReadManifest()
		require.NoError(t, err, "ReadManifest should not error on malformed content, only on a real read failure")
		assert.Equal(t, ManifestV1{}, got, "ReadManifest should return a zero ManifestV1 for malformed content")
	})

	t.Run("a real read failure is a hard error", func(t *testing.T) {
		dir := t.TempDir()

		mrw := NewManifestReadWriter(dir)

		path := filepath.Join(dir, manifestFileName)

		require.NoError(t, os.WriteFile(path, []byte("{}"), 0000), "should be able to write an unreadable manifest file")

		t.Cleanup(func() { _ = os.Chmod(path, 0644) })

		_, err := mrw.ReadManifest()
		require.Error(t, err, "ReadManifest should surface a real read failure (e.g. permission denied) rather than treat it as absent")
	})
}
