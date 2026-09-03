package main

import (
	"github.com/alecthomas/kong"

	"github.com/ascending-llc/jarvis-registry-cli/auth"
	"github.com/ascending-llc/jarvis-registry-cli/skills"
)

var version = "dev"

func main() {
	var cli struct {
		SyncSkills skills.SyncCommand `cmd:"" name:"sync-skills" help:"Sync skills against Jarvis Registry service."`
		Auth       struct {
			Login  auth.LoginCommand  `cmd:"" name:"login" help:"Log in to the Registry."`
			Status auth.StatusCommand `cmd:"" name:"status" help:"Show Registry authentication status."`
		} `cmd:"" name:"auth" help:"Manage Registry authentication."`
		Version kong.VersionFlag `help:"Print version and exit."`
	}

	ctx := kong.Parse(
		&cli,
		kong.Name("jarvis-registry"),
		kong.Description("Jarvis Registry CLI"),
		kong.UsageOnError(),
		kong.ConfigureHelp(kong.HelpOptions{Compact: true}),
		kong.Vars{"version": version},
	)

	err := ctx.Run()

	ctx.FatalIfErrorf(err)
}
