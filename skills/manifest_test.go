package skills

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManifestReadWriterWriteManifest(t *testing.T) {
	t.Run("round-trips through ReadSkills and leaves the file read-only with no temp litter", func(t *testing.T) {
		dir := t.TempDir()

		mrw := NewManifestReadWriter(dir)

		skills := []Metadata{{Id: "id-1", Name: "skill-1", Version: 1}}

		err := mrw.WriteManifest(skills)
		require.NoError(t, err, "WriteManifest should succeed writing a brand-new manifest file")

		stat, err := os.Stat(filepath.Join(dir, manifestFileName))
		require.NoError(t, err, "the manifest file should exist after WriteManifest")
		assert.Equal(t, fs.FileMode(0444), stat.Mode().Perm(), "the manifest file should be read-only after WriteManifest")

		got, err := mrw.ReadSkills()
		require.NoError(t, err, "ReadSkills should be able to read back what WriteManifest wrote")
		assert.Equal(t, skills, got, "ReadSkills should round-trip exactly what WriteManifest wrote")

		entries, err := os.ReadDir(dir)
		require.NoError(t, err, "should be able to list the registry directory after WriteManifest")
		assert.Len(t, entries, 1, "no temp file should be left behind after a successful WriteManifest")
	})

	t.Run("a pre-existing manifest survives a simulated write failure byte-for-byte", func(t *testing.T) {
		dir := t.TempDir()

		mrw := NewManifestReadWriter(dir)

		err := mrw.WriteManifest([]Metadata{{Id: "id-1", Name: "skill-1", Version: 1}})
		require.NoError(t, err, "should be able to write the initial manifest")

		originalBytes, err := os.ReadFile(filepath.Join(dir, manifestFileName))
		require.NoError(t, err, "should be able to read the initial manifest bytes")

		// Making the registry directory read-only forces WriteManifest's
		// os.CreateTemp call to fail before it ever touches mrw.path,
		// simulating a write that fails partway through.
		require.NoError(t, os.Chmod(dir, 0555), "should be able to make the registry directory read-only")

		t.Cleanup(func() { _ = os.Chmod(dir, 0755) })

		err = mrw.WriteManifest([]Metadata{{Id: "id-2", Name: "skill-2", Version: 2}})
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

		err := mrw.WriteManifest([]Metadata{{Id: "id-1", Name: "skill-1", Version: 1}})
		require.Error(t, err, "WriteManifest should surface the rename failure")

		stat, err := os.Stat(filepath.Join(dir, manifestFileName))
		require.NoError(t, err, "the pre-existing directory at the manifest path should still be there")
		assert.True(t, stat.IsDir(), "the pre-existing directory at the manifest path must be left untouched by the failed rename")

		entries, err := os.ReadDir(dir)
		require.NoError(t, err, "should be able to list the registry directory after the failed rename")
		assert.Len(t, entries, 1, "the failed rename's temp file should have been cleaned up")
	})
}
