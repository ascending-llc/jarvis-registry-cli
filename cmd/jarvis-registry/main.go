package main

import (
	"os"

	"github.com/alecthomas/kong"

	"github.com/ascending-llc/jarvis-registry-cli/auth"
	"github.com/ascending-llc/jarvis-registry-cli/cfg"
	"github.com/ascending-llc/jarvis-registry-cli/completion"
	"github.com/ascending-llc/jarvis-registry-cli/skills"
	"github.com/ascending-llc/jarvis-registry-cli/update"
)

var version = "dev"

func main() {
	var cli struct { //nolint:govet // fieldalignment: preserve the established command order in CLI help.
		Skills struct {
			Sync skills.SyncCommand `cmd:"" name:"sync" help:"Sync skills against Jarvis Registry service."`
			Show skills.ShowCommand `cmd:"" name:"show" help:"Show local skills sync settings."`
		} `cmd:"" name:"skills" help:"Manage local skills sync."`
		Configure cfg.ConfigureCommand `cmd:"" name:"configure" help:"Interactively configure the CLI, e.g. the Registry base URL."`
		Update    update.Command       `cmd:"" name:"update" help:"Download and install the latest jarvis-registry release, replacing the running binary."`
		Auth      struct {
			Login  auth.LoginCommand  `cmd:"" name:"login" help:"Log in to the Registry."`
			Status auth.StatusCommand `cmd:"" name:"status" help:"Show Registry authentication status."`
			Logout auth.LogoutCommand `cmd:"" name:"logout" help:"Log out of the Registry."`
		} `cmd:"" name:"auth" help:"Manage Registry authentication."`
		Completion completion.Command `cmd:"" name:"completion" help:"Print a shell completion script to stdout."`
		Version    kong.VersionFlag   `short:"v" help:"Print version and exit."`
	}

	// A bare invocation prints help and exits 0 rather than failing with kong's
	// usage-error exit code (80), which package validators such as winget's
	// treat as a broken executable.
	if len(os.Args) == 1 {
		os.Args = append(os.Args, "--help")
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
