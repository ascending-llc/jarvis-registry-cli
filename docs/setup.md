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

Download the archive for your OS and architecture from the
[GitHub releases page](https://github.com/ascending-llc/jarvis-registry-cli/releases), extract it,
and place `jarvis-registry` on your `PATH`. Repeat those steps to upgrade. Each archive includes the
scripts under `completions/` for optional manual installation.

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