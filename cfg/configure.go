package cfg

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
)

type (
	// Logger is the minimal logging interface required by ConfigureCommand,
	// satisfied by *log.Logger.
	Logger interface {
		Print(v ...any)
		Printf(format string, v ...any)
		Println(v ...any)
	}

	// ConfigureCommand implements the interactive "configure" subcommand.
	ConfigureCommand struct {
		logger      Logger
		stdin       io.Reader
		userHomeDir string
		registryDir string
	}

	configField struct { //nolint:govet // fieldalignment: keep the field definition in prompt-path-processing order.
		label     string
		path      []string
		normalize func(string) string
		validate  func(string) error
	}
)

var configurableFields = []configField{
	{
		label:     "Registry base URL",
		path:      []string{"registry", "base_url"},
		normalize: normalizeBaseURL,
		validate:  validateBaseUrl,
	},
}

// BeforeReset sets defaults for ConfigureCommand that do not depend on parsed
// command-line arguments.
func (c *ConfigureCommand) BeforeReset() (err error) {
	if c.userHomeDir, err = os.UserHomeDir(); err != nil {
		return fmt.Errorf("could not locate user home directory: %s", err.Error())
	}

	c.logger = log.New(os.Stdout, "", 0)
	c.stdin = os.Stdin

	return nil
}

// AfterApply derives the Registry configuration directory. It deliberately
// does not call Load because configure must also work before a config exists.
func (c *ConfigureCommand) AfterApply() error {
	c.registryDir = filepath.Join(c.userHomeDir, RegistryDirName)

	return nil
}

// Run prompts for every configurable field, validates the answers, and writes
// the completed configuration atomically after all fields succeed.
func (c *ConfigureCommand) Run() error {
	rw, err := NewConfigReadWriter(c.registryDir)
	if err != nil {
		return fmt.Errorf("failed to read configuration: %s", err.Error())
	}

	scanner := bufio.NewScanner(c.stdin)

	for _, field := range configurableFields {
		current := rw.Get(field.path)

		for {
			printPrompt(os.Stdout, field.label, current)

			if !scanner.Scan() {
				if err = scanner.Err(); err != nil {
					return fmt.Errorf("failed to read %s: %s", field.label, err.Error())
				}

				return fmt.Errorf("%s was left unconfigured: input ended before a valid value was provided", field.label)
			}

			value := strings.TrimSpace(scanner.Text())
			if value == "" {
				if current != "" {
					break
				}

				c.logger.Println("a value is required")

				continue
			}

			value = normalizeFieldValue(field, value)

			if err = field.validate(value); err != nil {
				c.logger.Println(err)

				continue
			}

			rw.Set(field.path, value)

			break
		}
	}

	if err = rw.Write(); err != nil {
		return fmt.Errorf("failed to write configuration: %s", err.Error())
	}

	c.logger.Printf("✓ Configuration saved to %s\n", rw.path)

	return nil
}

func printPrompt(out io.Writer, label string, current string) {
	if current == "" {
		_, _ = fmt.Fprintf(out, "%s: ", label)

		return
	}

	_, _ = fmt.Fprintf(out, "%s [%s]: ", label, current)
}

func normalizeFieldValue(field configField, value string) string {
	if field.normalize == nil {
		return value
	}

	return field.normalize(value)
}

func normalizeBaseURL(value string) string {
	if strings.Contains(value, "://") {
		return value
	}

	return "https://" + value
}
