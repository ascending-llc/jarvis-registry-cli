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

### Windows (winget)

```
winget install Ascending.JarvisRegistryCLI
```

Upgrade with `winget upgrade Ascending.JarvisRegistryCLI`.

### Linux

The installer supports Linux amd64 (x86_64) and arm64 (aarch64). The default per-user installation
does not require `sudo`. Install the latest release, the `jr` shorthand, and shell completions with:

```sh
curl -fsSL https://raw.githubusercontent.com/ascending-llc/jarvis-registry-cli/main/scripts/install.sh | bash
```

Review the [installer source](./scripts/install.sh) before running it. Re-running the command upgrades
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
3. `jarvis-registry skills sync [path]` — pulls the skills you have access to on the Registry down to
   your machine.

Run `jarvis-registry --help` or `jarvis-registry <command> --help` at any point for the full flag
reference.

## Skill sync modes

`skills sync` targets one of three AI coding agents, selected via `local.skills.mode` in config or
the `--mode` flag (the flag wins when both are set):

| Mode | Personal scope (no path) | Project scope (explicit path) | Skill invocation |
| --- | --- | --- | --- |
| `claude` | `~/.claude/skills/jarvis-registry/` | `<path>/.claude/skills/jarvis-registry/` | `jarvis-registry:<skill-name>` |
| `codex` | `~/.jarvis-registry/skills/codex/`, linked into `~/.codex/skills/` | `<path>/.agents/skills/` | `<skill-name>` |
| `copilot` | `~/.jarvis-registry/skills/copilot/`, linked into `~/.copilot/skills/` | `<path>/.github/skills/` | `<skill-name>` |

All three modes use personal scope when the path is omitted, making skills available across projects
without writing under any project directory. Codex/Copilot keep content in a CLI-owned directory and
create one link per Registry skill in the tool's personal skills directory (`~/.codex/skills/` or
`~/.copilot/skills/`). The built-in `sync-skills` wrapper is also linked, subject to the same default
collision protection and override/interactive replacement rules as other skills. On Windows these
are directory junctions; elsewhere they are symbolic links.

An explicit path selects project scope. Relative paths, including `.`, resolve against the current
working directory. Codex/Copilot still refuse an explicit home-directory path; omit the path instead
for personal scope. Project-scope Codex/Copilot sync retains its existing cleanup behavior: once the
CLI manages that directory, entries not tracked by its sync are removed.

## Configuration

`jarvis-registry configure` manages this file, created at `~/.jarvis-registry/config.yaml`:

| Field                     | Set by                        | Required | Description                                                                                                   |
| ------------------------- | ------------------------------ | -------- | --------------------------------------------------------------------------------------------------------------- |
| `registry.base_url`       | `configure`                    | yes      | Registry API origin (scheme + host only, e.g. `https://registry.acme.example.com`).                             |
| `registry.auth_base_url`  | hand-edit                      | no       | Overrides the OAuth origin if it differs from `base_url`. Only needed for local Registry development.           |
| `local.skills.mode`       | `configure`                    | see above| Default sync mode (`claude`, `codex`, or `copilot`), used when `--mode` isn't passed.                            |
| `local.skills.skip_ids`   | hand-edit                      | no       | Registry skill `Id`s (not names) that `skills sync` should never create, update, or keep synced locally.        |
| `local.skills.link.override` | hand-edit                   | no       | Default `false`. For personal-scope Codex/Copilot, replace existing files, folders, or links that collide with desired skill names. Replacing a real directory deletes its contents. |

`local.skills.skip_ids` is useful if you already maintain a personal copy of a skill you've since
published to the Registry under a different name — add its Registry `Id` here to keep only your
personal copy in sync and skip the duplicate.

Run `jarvis-registry skills show` to print the resolved `local.skills.mode` and
`local.skills.skip_ids` values without opening the config file directly. It also shows
`local.skills.link.override` when enabled. `configure` does not prompt for this setting.

## Authentication

- `jarvis-registry auth login` — ensures a valid Registry access token is cached, running the OAuth
  sign-in flow only if no valid or refreshable token is already cached.
- `jarvis-registry auth status` — prints the configured Registry base URL and whether you're currently
  logged in (plus granted token scopes). Exits with status `1` when not logged in, so it's safe to use
  in scripts.
- If you're already logged in but `auth status`/`skills sync` still reports "not logged in," and
  you're running from a restricted shell (an AI agent's sandboxed execution, a container, CI), retry
  from an unrestricted shell — macOS Keychain access failures in a sandbox surface as "not found," not
  "permission denied."

## Syncing skills

```
jarvis-registry skills sync [<project-path>] [--mode claude|codex|copilot] [-i|--interactive]
```

Examples:

```
# Claude Code, personal scope (into ~/.claude/skills/jarvis-registry/)
jarvis-registry skills sync --mode claude

# Claude Code, project scope
jarvis-registry skills sync --mode claude .

# Codex, personal scope (content in ~/.jarvis-registry/skills/codex/)
jarvis-registry skills sync --mode codex

# Codex, project scope
jarvis-registry skills sync --mode codex .

# GitHub Copilot, personal scope (content in ~/.jarvis-registry/skills/copilot/)
jarvis-registry skills sync --mode copilot

# GitHub Copilot, project scope
jarvis-registry skills sync --mode copilot .

# Decide whether to replace each conflicting personal skill from a terminal
jarvis-registry skills sync --mode copilot -i
```

Each run reconciles your local skills folder against what's currently available to you on the
Registry — creating new skills, updating changed ones, and removing ones you've lost access to — and
prints a summary table of what changed. It's safe to re-run at any time, e.g. on a schedule, to pick up
newly published or updated skills.

Personal-scope Codex/Copilot sync leaves existing entries at conflicting skill names untouched by
default. Its summary includes a `Link` column: `Linked`, `Unchanged`, `Relinked`, `Skipped`, `Removed`,
or `Failed`; `-` means no link operation was performed. Content may sync successfully while its link
is skipped or fails. Link failures cause a nonzero exit status; skipped collisions do not.

For a skipped collision, move or remove the conflicting entry yourself, re-run from a terminal with
`-i`/`--interactive`, or explicitly enable automatic replacement:

```yaml
local:
  skills:
    link:
      override: true
```

Replacement deletes the conflicting file or folder and its contents. Replacing a link removes only
the link, preserving its target. `-i` always asks, even when override is enabled; it is rejected when
stdin is not a terminal. Both settings have no effect in Claude or project scope.

When a synced skill is renamed or removed, the CLI cleans up links that point directly into its own
sync root whose targets no longer exist. Cleanup runs before link creation, so a desired skill name
pointing at a deleted owned target is repaired in the same run, without prompting. It leaves unrelated
entries and links to other locations alone.

The tool's personal skills directory must not resolve to the same directory as the CLI-owned content
root. Such a layout is rejected before content sync to prevent replacing skill content with links to
itself. If replacing a conflicting directory fails partway through deletion, any remaining contents
stay at the original path: fix the reported filesystem error and retry with replacement enabled.

## Development

Install the pre-commit hooks:

```
pre-commit install -t pre-commit
```

Use `make` for local development tasks (test, lint, fmt, etc.) — see the [Makefile](./Makefile) for all targets.
