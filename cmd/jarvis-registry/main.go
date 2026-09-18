package main

import (
	"github.com/alecthomas/kong"

	"github.com/ascending-llc/jarvis-registry-cli/auth"
	"github.com/ascending-llc/jarvis-registry-cli/cfg"
	"github.com/ascending-llc/jarvis-registry-cli/skills"
)

var version = "dev"

func main() {
	var cli struct { //nolint:govet // fieldalignment: preserve the established command order in CLI help.
		Skills struct {
			Sync skills.SyncCommand `cmd:"" name:"sync" help:"Sync skills against Jarvis Registry service."`
			Show skills.ShowCommand `cmd:"" name:"show" help:"Show local skills sync settings."`
		} `cmd:"" name:"skills" help:"Manage local skills sync."`
		Configure cfg.ConfigureCommand `cmd:"" name:"configure" help:"Interactively configure the CLI, e.g. the Registry base URL."`
		Auth      struct {
			Login  auth.LoginCommand  `cmd:"" name:"login" help:"Log in to the Registry."`
			Status auth.StatusCommand `cmd:"" name:"status" help:"Show Registry authentication status."`
		} `cmd:"" name:"auth" help:"Manage Registry authentication."`
		Version kong.VersionFlag `short:"v" help:"Print version and exit."`
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
