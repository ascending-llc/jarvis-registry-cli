package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveProjectPath(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err, "should be able to locate the user home directory")

	wd, err := os.Getwd()
	require.NoError(t, err, "should be able to locate the current working directory")

	cases := []struct {
		name        string
		projectPath string
		want        string
		wantErrText string
	}{
		{name: "empty defaults to home directory", projectPath: "", want: home},
		{name: "NUL byte is rejected", projectPath: "some\x00dir", wantErrText: "NUL byte"},
		{name: "already-absolute path is returned unchanged", projectPath: filepath.Join(wd, "abs", "dir"), want: filepath.Join(wd, "abs", "dir")},
		{name: "relative path is joined against the working directory", projectPath: filepath.Join("some", "dir"), want: filepath.Join(wd, "some", "dir")},
		{name: "dot resolves to the working directory", projectPath: ".", want: wd},
		{name: "dot-slash resolves to the working directory", projectPath: "./", want: wd},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := resolveProjectPath(c.projectPath)

			if c.wantErrText != "" {
				require.Error(t, err, "resolveProjectPath(%q) should return an error", c.projectPath)
				assert.Contains(t, err.Error(), c.wantErrText, "error message should explain why the path is invalid")

				return
			}

			require.NoError(t, err, "resolveProjectPath(%q) should succeed", c.projectPath)
			assert.Equal(t, c.want, got, "resolveProjectPath(%q) mismatch", c.projectPath)
		})
	}
}
