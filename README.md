# jarvis-registry-cli

Companion CLI for [Jarvis Registry](https://github.com/ascending-llc/jarvis-registry): syncs the AI
coding skills your organization curates in the Registry down into your local Claude Code, Codex, or
GitHub Copilot workspace.

## Installation

### macOS (Homebrew)

```
brew tap ascending-llc/jarvis
brew install ascending-llc/jarvis/jarvis-registry
```

Upgrade with `brew upgrade jarvis-registry`. Shell completions (bash, zsh, fish) are installed
automatically; on a fresh install, restart your shell (or open a new terminal) before they take effect.

### Linux / Windows

Download the archive for your OS+architecture from the
[releases page](https://github.com/ascending-llc/jarvis-registry-cli/releases), extract it, and place
the `jarvis-registry` (or `jarvis-registry.exe`) binary on your `PATH`. Repeat the same steps to
upgrade. Each archive also bundles the `completions/` scripts if you want to source them manually.

### Go toolchain

If you have the Go toolchain installed and `GOBIN` on your `PATH`:

```
go install github.com/ascending-llc/jarvis-registry-cli/cmd/jarvis-registry@latest
```

Note that a CLI installed this way always reports `dev` for `jarvis-registry --version`.

## Getting started

Run these once, in order:

1. `jarvis-registry configure` — interactively set the Registry base URL (ask your Registry admin
   for it) and pick your skills sync mode (see below). Values are saved to
   `~/.jarvis-registry/config.yaml`; re-run `configure` any time to change them.
2. `jarvis-registry auth login` — opens a browser to complete sign-in against your organization's
   Registry. The resulting credentials are cached in your OS keyring (Keychain, Windows Credential
   Manager, etc.) — never written to disk in plain text. Re-running it is a no-op once a valid
   session is cached.
3. `jarvis-registry sync-skills [path]` — pulls the skills you have access to on the Registry down to
   your machine.

Run `jarvis-registry --help` or `jarvis-registry <command> --help` at any point for the full flag
reference.

## Skill sync modes

`sync-skills` targets one of three AI coding agents, selected via `local.skills.mode` in config or
the `--mode` flag (the flag wins when both are set):

| Mode     | Destination folder                                  | Scope                        | Skill invocation           |
| -------- | ---------------------------------------------------- | ----------------------------- | --------------------------- |
| `claude` | `<path>/.claude/skills/jarvis-registry/`             | personal (default) or project | `jarvis-registry:<skill-name>` |
| `codex`  | `<path>/.agents/skills/`                              | project only                  | `<skill-name>`               |
| `copilot`| `<path>/.github/skills/`                              | project only                  | `<skill-name>`               |

`claude` mode is the only one with a personal scope: omit the path argument and skills sync into your
home directory, available to Claude Code across every project. `codex` and `copilot` always require a
project directory (relative paths, including `.`, resolve against your current working directory) and
refuse to target your home directory.

## Configuration

`jarvis-registry configure` manages this file, created at `~/.jarvis-registry/config.yaml`:

| Field                     | Set by                        | Required | Description                                                                                                   |
| ------------------------- | ------------------------------ | -------- | --------------------------------------------------------------------------------------------------------------- |
| `registry.base_url`       | `configure`                    | yes      | Registry API origin (scheme + host only, e.g. `https://registry.acme.example.com`).                             |
| `registry.auth_base_url`  | hand-edit                      | no       | Overrides the OAuth origin if it differs from `base_url`. Only needed for local Registry development.           |
| `local.skills.mode`       | `configure`                    | see above| Default sync mode (`claude`, `codex`, or `copilot`), used when `--mode` isn't passed.                            |
| `local.skills.skip_ids`   | hand-edit                      | no       | Registry skill `Id`s (not names) that `sync-skills` should never create, update, or keep synced locally.        |

`local.skills.skip_ids` is useful if you already maintain a personal copy of a skill you've since
published to the Registry under a different name — add its Registry `Id` here to keep only your
personal copy in sync and skip the duplicate.

## Authentication

- `jarvis-registry auth login` — ensures a valid Registry access token is cached, running the OAuth
  sign-in flow only if no valid or refreshable token is already cached.
- `jarvis-registry auth status` — prints the configured Registry base URL and whether you're currently
  logged in (plus granted token scopes). Exits with status `1` when not logged in, so it's safe to use
  in scripts.

## Syncing skills

```
jarvis-registry sync-skills [<project-path>] [--mode claude|codex|copilot]
```

Examples:

```
# Claude Code, personal scope (into ~/.claude/skills/jarvis-registry/)
jarvis-registry sync-skills --mode claude

# Claude Code, project scope
jarvis-registry sync-skills --mode claude .

# Codex — project directory is required
jarvis-registry sync-skills --mode codex .

# GitHub Copilot — project directory is required
jarvis-registry sync-skills --mode copilot .
```

Each run reconciles your local skills folder against what's currently available to you on the
Registry — creating new skills, updating changed ones, and removing ones you've lost access to — and
prints a summary table of what changed. It's safe to re-run at any time, e.g. on a schedule, to pick up
newly published or updated skills.

## Development

Install the pre-commit hooks:

```
pre-commit install -t pre-commit
```

Use `make` for local development tasks (test, lint, fmt, etc.) — see the [Makefile](./Makefile) for all targets.
