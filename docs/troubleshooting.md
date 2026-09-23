# Troubleshooting

## Authentication appears missing

If `jarvis-registry auth status` or `jarvis-registry skills sync` reports that you are not logged in
after a successful login, check whether the command is running in a restricted shell such as an AI
agent sandbox, container, or CI environment.

On macOS, a sandboxed Keychain access failure can appear as "not found" rather than "permission
denied." Retry from an unrestricted terminal. Credentials are stored in the OS keyring and are never
written to disk in plain text.

## Interactive sync is rejected

`--interactive` and `-i` require a terminal because the CLI must ask before replacing each
conflicting personal skill. Run the command from a terminal, or omit interactive mode and configure
automatic replacement as described under [Personal skill collisions](getting-started.md#personal-skill-collisions).

## A personal skill link is skipped

Personal-scope Codex and GitHub Copilot sync do not replace an existing file, directory, or link with
the desired skill name by default. Move or remove the conflicting entry, rerun with `--interactive`,
or enable `local.skills.link.override`.

Replacement of a real directory deletes its contents. Replacement of a link removes only the link
and preserves its target.

## A link fails

Content can sync successfully even when a personal Codex or GitHub Copilot link fails. The summary
reports `Failed`, and the command exits with a nonzero status. Fix the reported filesystem error and
rerun the same sync command.

If replacing a directory fails partway through deletion, any remaining contents stay at the original
path. Resolve the filesystem error before retrying with replacement enabled.

## A project path is refused

For Codex and GitHub Copilot, an explicit home-directory path is not valid project scope. Omit the
path to use personal scope instead:

```sh
jarvis-registry skills sync --mode codex
jarvis-registry skills sync --mode copilot
```

## The personal skills directory conflicts with the sync root

The agent's personal skills directory must not resolve to the CLI-owned content root. The CLI rejects
this layout before content sync because replacing content with links to itself could destroy synced
skills. Restore the normal agent skills location or CLI-owned content location, then retry.

See [Getting started](getting-started.md) for integration paths, scope behavior, and configuration.