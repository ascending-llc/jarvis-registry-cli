package main

import (
	"github.com/alecthomas/kong"

	"github.com/ascending-llc/jarvis-registry-cli/skills"
)

func main() {
	var cli struct {
		SyncSkills skills.SyncCommand `cmd:"" name:"sync-skills" help:"Sync skills against Jarvis Registry service."`
	}

	ctx := kong.Parse(
		&cli,
		kong.Name("jarvis-registry"),
		kong.Description("Jarvis Registry CLI"),
		kong.UsageOnError(),
		kong.ConfigureHelp(kong.HelpOptions{Compact: true}),
	)

	err := ctx.Run()

	ctx.FatalIfErrorf(err)
}
