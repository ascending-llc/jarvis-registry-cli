// Package completion prints the shell completion scripts embedded in the CLI.
package completion

import (
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"os"
)

// Command prints a completion script for the selected shell.
type Command struct {
	out   io.Writer
	Shell string `arg:"" enum:"bash,zsh,fish,powershell" help:"Shell to print a completion script for: ${enum}."`
}

var (
	//go:embed jarvis-registry.bash
	bashScript []byte

	//go:embed jarvis-registry.zsh
	zshScript []byte

	//go:embed jarvis-registry.fish
	fishScript []byte

	//go:embed jr.fish
	jrFishScript []byte

	//go:embed jarvis-registry.ps1
	powershellScript []byte
)

// BeforeReset initializes the script output before Kong parses arguments.
func (c *Command) BeforeReset() error {
	c.out = os.Stdout

	return nil
}

// Run writes the selected script verbatim, including both fish registrations.
func (c *Command) Run() error {
	var scripts [][]byte

	switch c.Shell {
	case "bash":
		scripts = [][]byte{bashScript}
	case "zsh":
		scripts = [][]byte{zshScript}
	case "fish":
		scripts = [][]byte{fishScript, jrFishScript}
	case "powershell":
		scripts = [][]byte{powershellScript}
	default:
		return fmt.Errorf("unsupported shell %q", c.Shell)
	}

	for _, script := range scripts {
		if _, err := io.Copy(c.out, bytes.NewReader(script)); err != nil {
			return fmt.Errorf("failed to write %s completion script: %s", c.Shell, err.Error())
		}
	}

	return nil
}
