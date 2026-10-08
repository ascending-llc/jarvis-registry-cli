package completion

import (
	"bytes"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type (
	failingWriter struct {
		failOn int
		calls  int
	}

	shortWriter struct{}
)

func (w *failingWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.calls == w.failOn {
		return 0, errors.New("broken pipe")
	}

	return len(p), nil
}

func (shortWriter) Write(p []byte) (int, error) {
	return len(p) - 1, nil
}

func TestCommandRun(t *testing.T) {
	for _, tc := range []struct {
		shell string
		files []string
	}{
		{shell: "bash", files: []string{"jarvis-registry.bash"}},
		{shell: "zsh", files: []string{"jarvis-registry.zsh"}},
		{shell: "fish", files: []string{"jarvis-registry.fish", "jr.fish"}},
		{shell: "powershell", files: []string{"jarvis-registry.ps1"}},
	} {
		t.Run(tc.shell, func(t *testing.T) {
			var want []byte
			for _, name := range tc.files {
				content, err := os.ReadFile(name)
				require.NoError(t, err)
				require.NotEmpty(t, content)
				assert.NotContains(t, string(content), "\r", "embedded scripts must use LF")
				assert.Equal(t, byte('\n'), content[len(content)-1], "scripts must end with LF")

				for _, b := range content {
					require.Less(t, b, byte(128), "scripts must remain ASCII")
				}

				want = append(want, content...)
			}

			var out bytes.Buffer

			cmd := Command{Shell: tc.shell, out: &out}
			require.NoError(t, cmd.Run())
			assert.Equal(t, want, out.Bytes())
		})
	}
}

func TestCommandKongLifecycle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	var cli struct {
		Completion Command `cmd:""`
	}

	parser, err := kong.New(&cli)
	require.NoError(t, err)
	ctx, err := parser.Parse([]string{"completion", "bash"})
	require.NoError(t, err)
	assert.Same(t, os.Stdout, cli.Completion.out)

	var out bytes.Buffer

	cli.Completion.out = &out

	require.NoError(t, ctx.Run())

	want, err := os.ReadFile("jarvis-registry.bash")
	require.NoError(t, err)
	assert.Equal(t, want, out.Bytes())

	entries, err := os.ReadDir(home)
	require.NoError(t, err)
	assert.Empty(t, entries, "completion must not create config or credentials")
}

func TestCommandInvalidArguments(t *testing.T) {
	for _, tc := range []struct {
		name string
		want string
		args []string
	}{
		{name: "missing shell", args: []string{"completion"}, want: "<shell>"},
		{name: "unsupported shell", args: []string{"completion", "tcsh"}, want: "must be one of"},
		{name: "no pwsh alias", args: []string{"completion", "pwsh"}, want: "must be one of"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cli struct {
				Completion Command `cmd:""`
			}

			parser, err := kong.New(&cli)
			require.NoError(t, err)
			_, err = parser.Parse(tc.args)
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestCommandWriteFailure(t *testing.T) {
	for _, failOn := range []int{1, 2} {
		out := &failingWriter{failOn: failOn}
		cmd := Command{Shell: "fish", out: out}
		require.EqualError(t, cmd.Run(), "failed to write fish completion script: broken pipe")
		assert.Equal(t, failOn, out.calls, "stop at the first failed write")
	}
}

func TestCommandShortWrite(t *testing.T) {
	cmd := Command{Shell: "bash", out: shortWriter{}}
	require.EqualError(t, cmd.Run(), "failed to write bash completion script: "+io.ErrShortWrite.Error())
}

func TestCommandRunUnsupportedShell(t *testing.T) {
	var out bytes.Buffer

	cmd := Command{Shell: "tcsh", out: &out}
	require.ErrorContains(t, cmd.Run(), "unsupported shell")
	assert.Empty(t, out.String())
}
