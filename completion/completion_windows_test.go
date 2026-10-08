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

	// line is the text before the cursor and after is the text following it. path marks cases
	// whose matches are filesystem paths, compared by base name.
	tests := []struct {
		name  string
		line  string
		after string
		want  []string
		path  bool
	}{
		{name: "root", line: "jarvis-registry sk", want: []string{"skills"}},
		{name: "exe", line: "jarvis-registry.exe sk", want: []string{"skills"}},
		{name: "root_completion", line: "jarvis-registry comp", want: []string{"completion"}},
		{name: "root_update", line: "jarvis-registry up", want: []string{"update"}},
		{name: "auth", line: "jarvis-registry auth lo", want: []string{"login", "logout"}},
		{name: "skills", line: "jarvis-registry skills s", want: []string{"sync", "show"}},
		{name: "sync_flags", line: "jarvis-registry skills sync --", want: []string{"--help", "--mode", "--interactive"}},
		{name: "sync_help_prefix", line: "jarvis-registry skills sync --h", want: []string{"--help"}},
		{name: "sync_mode_flag", line: "jarvis-registry skills sync --m", want: []string{"--mode"}},
		{name: "sync_interactive_flag", line: "jarvis-registry skills sync --i", want: []string{"--interactive"}},
		{name: "mode_values", line: "jarvis-registry skills sync --mode ", want: []string{"claude", "codex", "copilot"}},
		{name: "mode_prefix", line: "jarvis-registry skills sync --mode co", want: []string{"codex", "copilot"}},
		{name: "completion_shells", line: "jarvis-registry completion ", want: []string{"-h", "--help", "bash", "zsh", "fish", "powershell"}},
		{name: "powershell", line: "jarvis-registry completion p", want: []string{"powershell"}},
		{name: "unknown_shell", line: "jarvis-registry completion tcsh", want: []string{}},
		{name: "completion_help", line: "jarvis-registry completion bash --", want: []string{"--help"}},
		{name: "configure_help", line: "jarvis-registry configure --", want: []string{"--help"}},
		{name: "show_help", line: "jarvis-registry skills show --", want: []string{"--help"}},
		{name: "completion_help_prefix", line: "jarvis-registry completion bash --h", want: []string{"--help"}},
		{name: "configure_help_prefix", line: "jarvis-registry configure --h", want: []string{"--help"}},
		{name: "show_help_prefix", line: "jarvis-registry skills show --h", want: []string{"--help"}},
		{name: "update_flag", line: "jarvis-registry update --ch", want: []string{"--check"}},
		{name: "middle_of_word", line: "jarvis-registry completion p", after: "bad", want: []string{"powershell"}},
		{name: "middle_of_line", line: "jarvis-registry sk", after: " --help", want: []string{"skills"}},
		{name: "middle_mode", line: "jarvis-registry skills sync --mode co", after: " ./project", want: []string{"codex", "copilot"}},
		{name: "directory", line: "jarvis-registry skills sync ./proj", want: []string{"project space"}, path: true},
		{name: "directory_after_mode", line: "jarvis-registry skills sync --mode codex ./proj", want: []string{"project space"}, path: true},
		{name: "directory_after_separator", line: "jarvis-registry skills sync -- ./proj", want: []string{"project space"}, path: true},
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
							// https://github.com/PowerShell/PowerShell/issues/2912
							if engine == "powershell.exe" && strings.HasSuffix(test.line, " --") {
								t.Skip("PowerShell 5.1 does not invoke native completers for bare --")
							}

							matches := powerShellCompletions(t, engine, mode, scriptPath, test.line+test.after, len(test.line))
							if test.path {
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
$matches = @((TabExpansion2 -inputScript $inputCase.line -cursorColumn $inputCase.cursor).CompletionMatches |
    ForEach-Object { $_.CompletionText })
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
