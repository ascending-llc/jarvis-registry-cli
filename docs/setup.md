# Setup

Install the CLI, obtain your Registry URL from your administrator, and configure a default skills
integration before following [Getting started](getting-started.md).

## macOS

Install with Homebrew:

```sh
brew tap ascending-llc/jarvis
brew install ascending-llc/jarvis/jarvis-registry
```

Upgrade with `brew upgrade jarvis-registry`. Bash, zsh, and fish completions are installed
automatically. After a fresh installation, restart your shell before using them.

## Windows

Install with winget:

```powershell
winget install Ascending.JarvisRegistryCLI
```

Upgrade with `winget upgrade Ascending.JarvisRegistryCLI`.

## Linux

The installer supports Linux amd64 (x86_64) and arm64 (aarch64). The default per-user installation
does not require `sudo`. Install the latest release, the `jr` shorthand, and shell completions with:

```sh
curl -fsSL https://raw.githubusercontent.com/ascending-llc/jarvis-registry-cli/main/scripts/install.sh | bash
```

Review the [installer source](../scripts/install.sh) before running it. Re-running the command upgrades
an existing installation. The default binary directory is `~/.local/bin`; the installer prints any
needed `PATH` and zsh completion setup instructions without changing your shell configuration.
Bash completion requires `bash-completion` v2 to be installed and sourced by your shell. Completion
paths honor `XDG_DATA_HOME` (bash/zsh) and `XDG_CONFIG_HOME` (fish); some versions of `bash-completion`
cannot auto-load completions when `XDG_DATA_HOME` contains spaces.

To choose a different directory or a particular release, set variables on the `bash` side of the pipe:

```sh
curl -fsSL https://raw.githubusercontent.com/ascending-llc/jarvis-registry-cli/main/scripts/install.sh |
  BINDIR="$HOME/bin" JARVIS_REGISTRY_VERSION=v0.6.6 bash
```

Replace the example version with the release you want. Setting `JARVIS_REGISTRY_VERSION` skips the
GitHub latest-release API lookup, so it also works around failures at that step, such as API rate
limiting. It still requires access to the installer URL and GitHub release downloads.

You can also install an archive manually from the
[releases page](https://github.com/ascending-llc/jarvis-registry-cli/releases).

## Go toolchain

If the Go toolchain is installed and `GOBIN` is on your `PATH`:

```sh
go install github.com/ascending-llc/jarvis-registry-cli/cmd/jarvis-registry@latest
```

A CLI installed this way always reports `dev` for `jarvis-registry --version`.

## Configure the CLI

Run the interactive configuration command:

```sh
jarvis-registry configure
```

Enter the Registry base URL supplied by your administrator and choose `claude`, `codex`, or
`copilot` as the default skills sync mode. The CLI writes these values to
`~/.jarvis-registry/config.yaml`; rerun `configure` whenever they need to change.

Confirm the installation and review available commands:

```sh
jarvis-registry --version
jarvis-registry --help
```

Continue with [Getting started](getting-started.md) to sign in and sync skills.
