package completion

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPowerShellCompletion(t *testing.T) {
	dir := t.TempDir()

	var output bytes.Buffer

	command := Command{Shell: "powershell", out: &output}
	require.NoError(t, command.Run())

	scriptPath := filepath.Join(dir, "completion.ps1")
	require.NoError(t, os.WriteFile(scriptPath, output.Bytes(), 0600))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "project space"), 0700))

	tests := []struct {
		name string
		line string
		want []string
	}{
		{"root", "jarvis-registry sk", []string{"skills"}},
		{"exe", "jarvis-registry.exe sk", []string{"skills"}},
		{"root_completion", "jarvis-registry comp", []string{"completion"}},
		{"root_update", "jarvis-registry up", []string{"update"}},
		{"auth", "jarvis-registry auth lo", []string{"login", "logout"}},
		{"skills", "jarvis-registry skills s", []string{"sync", "show"}},
		{"sync_flags", "jarvis-registry skills sync --", []string{"--help", "--mode", "--interactive"}},
		{"mode_values", "jarvis-registry skills sync --mode ", []string{"claude", "codex", "copilot"}},
		{"mode_prefix", "jarvis-registry skills sync --mode co", []string{"codex", "copilot"}},
		{"completion_shells", "jarvis-registry completion ", []string{"-h", "--help", "bash", "zsh", "fish", "powershell"}},
		{"powershell", "jarvis-registry completion p", []string{"powershell"}},
		{"completion_help", "jarvis-registry completion bash --", []string{"--help"}},
		{"configure_help", "jarvis-registry configure --", []string{"--help"}},
		{"show_help", "jarvis-registry skills show --", []string{"--help"}},
		{"update_flag", "jarvis-registry update --ch", []string{"--check"}},
		{"middle_of_word", "jarvis-registry completion p|bad", []string{"powershell"}},
		{"middle_of_line", "jarvis-registry sk| --help", []string{"skills"}},
		{"middle_mode", "jarvis-registry skills sync --mode co| ./project", []string{"codex", "copilot"}},
		{"directory", "jarvis-registry skills sync ./proj", []string{"project space"}},
		{"directory_after_mode", "jarvis-registry skills sync --mode codex ./proj", []string{"project space"}},
		{"directory_after_separator", "jarvis-registry skills sync -- ./proj", []string{"project space"}},
	}

	for _, engine := range []string{"powershell.exe", "pwsh.exe"} {
		t.Run(engine, func(t *testing.T) {
			_, err := exec.LookPath(engine)
			if err != nil && engine == "pwsh.exe" && os.Getenv("CI") == "" {
				t.Skip("PowerShell 7 is not installed; both engines are required in CI")
			}

			require.NoError(t, err, "PowerShell engine must be installed")

			for _, mode := range []string{"FullLanguage", "ConstrainedLanguage"} {
				t.Run(mode, func(t *testing.T) {
					t.Parallel()

					for _, test := range tests {
						t.Run(test.name, func(t *testing.T) {
							line := strings.Replace(test.line, "|", "", 1)

							cursor := strings.Index(test.line, "|")
							if cursor < 0 {
								cursor = len(line)
							}

							matches := powerShellCompletions(t, engine, mode, scriptPath, line, cursor)
							if strings.HasPrefix(test.name, "directory") {
								for i, match := range matches {
									matches[i] = filepath.Base(strings.TrimRight(strings.Trim(match, "'\""), `/\`))
								}
							}

							require.ElementsMatch(t, test.want, matches)
						})
					}
				})
			}
		})
	}
}

func powerShellCompletions(t *testing.T, engine, mode, scriptPath, line string, cursor int) []string {
	t.Helper()

	input, err := json.Marshal(struct {
		Line   string `json:"line"`
		Cursor int    `json:"cursor"`
	}{Line: line, Cursor: cursor})
	require.NoError(t, err)

	const script = `
$ErrorActionPreference = 'Stop'
$inputCase = $env:JARVIS_COMPLETION_INPUT | ConvertFrom-Json
$ExecutionContext.SessionState.LanguageMode = $env:JARVIS_COMPLETION_LANGUAGE
Get-Content -LiteralPath $env:JARVIS_COMPLETION_SCRIPT -Raw | Invoke-Expression
$matches = @((TabExpansion2 -inputScript $inputCase.line -cursorColumn $inputCase.cursor).CompletionMatches.CompletionText)
ConvertTo-Json -InputObject $matches -Compress
`

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, engine, "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.Dir = filepath.Dir(scriptPath)
	cmd.Env = append(os.Environ(),
		"JARVIS_COMPLETION_INPUT="+string(input),
		"JARVIS_COMPLETION_LANGUAGE="+mode,
		"JARVIS_COMPLETION_SCRIPT="+scriptPath,
	)

	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)

	var matches []string
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(output), &matches), "%s", output)

	return matches
}
