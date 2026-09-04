package skills

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// resolveProjectPath resolves projectPath to an absolute directory: an
// empty value (ProjectPath left unset) defaults to the user's home
// directory, so the plugin loads in personal scope by default; any other
// value is resolved with filepath.Abs, which leaves an already-absolute
// path as Clean(path) and joins a relative one against the current
// working directory — so "." (or "./") means "here".
func resolveProjectPath(projectPath string) (string, error) {
	projectPath = strings.TrimSpace(projectPath)

	if strings.ContainsRune(projectPath, 0) {
		return "", errors.New("path must not contain a NUL byte")
	}

	if projectPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("could not locate user home directory: %s", err.Error())
		}

		return home, nil
	}

	abs, err := filepath.Abs(projectPath)
	if err != nil {
		return "", fmt.Errorf("could not resolve path %q: %s", projectPath, err.Error())
	}

	return abs, nil
}
