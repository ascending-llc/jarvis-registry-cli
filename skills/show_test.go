package skills

import (
	"bytes"
	"errors"
	"log"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ascending-llc/jarvis-registry-cli/cfg"
)

func TestShowCommandBeforeReset(t *testing.T) {
	cmd := ShowCommand{}

	err := cmd.BeforeReset()
	require.NoError(t, err, "should be able to call ShowCommand.BeforeReset without error")

	logger, ok := cmd.logger.(*log.Logger)
	require.True(t, ok, "logger should be a *log.Logger")
	assert.Empty(t, logger.Prefix(), "logger should not have a prefix, so its output isn't run together with unprefixed messages")
}

func TestShowCommandRun(t *testing.T) {
	cases := []struct {
		name       string
		mode       cfg.SkillsMode
		wantOutput string
		skipIds    []string
		override   bool
	}{
		{name: "mode set and skip_ids set", mode: cfg.SkillsModeClaude, skipIds: []string{"skill-1", "skill-2"}, wantOutput: "Skill sync mode: claude\nSkip IDs:\n  - skill-1\n  - skill-2\n"},
		{name: "mode unset prints placeholder", mode: "", skipIds: []string{"skill-1"}, wantOutput: "Skill sync mode: (not set)\nSkip IDs:\n  - skill-1\n"},
		{name: "skip_ids empty omits skip IDs line", mode: cfg.SkillsModeCodex, skipIds: nil, wantOutput: "Skill sync mode: codex\n"},
		{name: "override shown even without skip ids", mode: cfg.SkillsModeCodex, override: true, wantOutput: "Skill sync mode: codex\nlocal.skills.link.override: true\n"},
		{name: "override shown before skip ids", mode: cfg.SkillsModeCopilot, skipIds: []string{"skill-1"}, override: true, wantOutput: "Skill sync mode: copilot\nlocal.skills.link.override: true\nSkip IDs:\n  - skill-1\n"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cmd := ShowCommand{}

			require.NoError(t, cmd.BeforeReset(), "should be able to call ShowCommand.BeforeReset without error")

			out := &bytes.Buffer{}
			cmd.logger = log.New(out, "", 0)

			cmd.configLoadFunc = func(string) (cfg.Config, error) {
				var config cfg.Config

				config.Local.Skills.Mode = c.mode
				config.Local.Skills.SkipIds = c.skipIds
				config.Local.Skills.Link.Override = c.override

				return config, nil
			}

			require.NoError(t, cmd.AfterApply(), "should be able to call ShowCommand.AfterApply without error")

			require.NoError(t, cmd.Run(), "Run should not return an error")

			assert.Equal(t, c.wantOutput, out.String())
		})
	}
}

func TestShowCommandAfterApplyConfigError(t *testing.T) {
	cmd := ShowCommand{}

	require.NoError(t, cmd.BeforeReset(), "should be able to call ShowCommand.BeforeReset without error")

	cmd.configLoadFunc = func(string) (cfg.Config, error) {
		return cfg.Config{}, errors.New("boom")
	}

	err := cmd.AfterApply()
	require.Error(t, err, "AfterApply should surface a config load failure")
	assert.Contains(t, err.Error(), "failed to load config options: boom", "error should be wrapped with the command's own context")
}
