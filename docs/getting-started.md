# Getting started

Complete the [setup guide](setup.md), then authenticate with your organization's Registry:

```sh
jarvis-registry auth login
```

The browser-based sign-in normally stores credentials in your OS keyring. On Linux without a
usable keyring, including containers and minimal WSL2 environments, the CLI automatically stores
them in plaintext at `~/.jarvis-registry/credentials.json` with mode `0600` and prints a warning
to stderr when it first creates the service's entry. Later token refreshes update that entry
without repeating the warning. A file entry takes precedence over any old keyring entry.

Check the current session, credentials location, and granted token scopes with
`jarvis-registry auth status`. The status command exits with status `1` when you are not logged in,
so it can be used in scripts. macOS and Windows use the keyring by default; automatic fallback
is Linux-only.

Run `jarvis-registry auth logout` to remove the configured auth server's cached credentials from
both stores. Other servers' entries are preserved. This clears local credentials; it does not
revoke server-side tokens or end browser SSO sessions. Logout exits with status `1` when no
credentials are cached. A subsequent login retries the keyring unless file-only mode is enabled.

To use the plaintext file explicitly on any OS, hand-edit `~/.jarvis-registry/config.yaml`:

```yaml
local:
  credentials:
    file_only: true
```

This setting bypasses keyring reads and writes without a warning. Logout still attempts keyring
cleanup. Use YAML `true` or `false`, not quoted strings or numbers. `configure` does not prompt
for this setting. The file is not encrypted; Unix file permissions restrict access to its owner.

## Claude Code

Sync skills for use across all Claude Code projects:

```sh
jarvis-registry skills sync --mode claude
```

Personal skills are stored under `~/.claude/skills/jarvis-registry/` and invoked as
`jarvis-registry:<skill-name>`.

To sync only for a project, pass its path:

```sh
jarvis-registry skills sync --mode claude .
```

Project skills are stored under `<path>/.claude/skills/jarvis-registry/`.

## Codex

Sync skills for use across all Codex projects:

```sh
jarvis-registry skills sync --mode codex
```

Personal skill content is stored under `~/.jarvis-registry/skills/codex/`, with one link per skill
under `~/.codex/skills/`. On Windows these links are directory junctions; elsewhere they are symbolic
links. Invoke a synced skill by its `<skill-name>`.

To sync only for a project, pass its path:

```sh
jarvis-registry skills sync --mode codex .
```

Project skills are stored under `<path>/.agents/skills/`. An explicit home-directory path is refused;
omit the path to use personal scope.

## GitHub Copilot

Sync skills for use across all GitHub Copilot projects:

```sh
jarvis-registry skills sync --mode copilot
```

Personal skill content is stored under `~/.jarvis-registry/skills/copilot/`, with one link per skill
under `~/.copilot/skills/`. On Windows these links are directory junctions; elsewhere they are symbolic
links. Invoke a synced skill by its `<skill-name>`.

To sync only for a project, pass its path:

```sh
jarvis-registry skills sync --mode copilot .
```

Project skills are stored under `<path>/.github/skills/`. An explicit home-directory path is refused;
omit the path to use personal scope.

## Sync scope and mode

Omitting the path selects personal scope, making skills available across projects without writing
under a project directory. An explicit path selects project scope; relative paths, including `.`,
resolve against the current working directory. Once the CLI manages a project-scope Codex or GitHub
Copilot skills directory, entries not tracked by its sync are removed.

The `--mode` flag overrides `local.skills.mode` from the config file. If the mode is already
configured, the shorter command is enough:

```sh
jarvis-registry skills sync
```

Each run creates new skills, updates changed skills, removes skills you have lost access to, and
prints a summary table. It is safe to run on a schedule. Run `jarvis-registry skills show` to inspect
the resolved mode, skipped skill IDs, and link override setting.

## Configuration reference

The config file is stored at `~/.jarvis-registry/config.yaml`.

| Field | Set by | Required | Description |
| --- | --- | --- | --- |
| `registry.base_url` | `configure` | yes | Registry API origin, such as `https://registry.acme.example.com`. |
| `registry.auth_base_url` | hand-edit | no | OAuth origin override for local Registry development. |
| `local.credentials.file_only` | hand-edit | no | Always store credentials in `~/.jarvis-registry/credentials.json` instead of the OS keyring. Defaults to `false`. |
| `local.skills.mode` | `configure` | unless `--mode` is passed | Default mode: `claude`, `codex`, or `copilot`. |
| `local.skills.skip_ids` | hand-edit | no | Registry skill IDs that must not be created, updated, or retained locally. |
| `local.skills.link.override` | hand-edit | no | Replace personal-scope Codex/Copilot entries that collide with desired skill names. Defaults to `false`. |

Use `local.skills.skip_ids` when you maintain a personal copy of a published Registry skill and want
to skip the duplicate by its Registry ID.

## Personal skill collisions

Personal-scope Codex and Copilot sync leave existing entries with conflicting names untouched by
default. The summary's `Link` column reports `Linked`, `Unchanged`, `Relinked`, `Skipped`, `Removed`,
or `Failed`; `-` means no link operation was performed. Content can sync successfully while its link
is skipped or fails. A failed link causes a nonzero exit status; a skipped collision does not.

To decide whether to replace each conflict from a terminal, use interactive mode:

```sh
jarvis-registry skills sync --mode copilot --interactive
```

To replace conflicts automatically, edit the config file:

```yaml
local:
  skills:
    link:
      override: true
```

Replacement deletes a conflicting file or real directory and its contents. Replacing a link removes
only the link and preserves its target. Interactive mode always asks, even when override is enabled.
Both settings have no effect in Claude or project scope.

The built-in `sync-skills` wrapper follows the same collision rules as other skills. When a synced
skill is renamed or removed, the CLI removes links that point directly into its own sync root and
whose targets no longer exist. It leaves unrelated entries and links to other locations untouched.

See [Troubleshooting](troubleshooting.md) when authentication, path validation, collision handling,
or link creation prevents a sync.