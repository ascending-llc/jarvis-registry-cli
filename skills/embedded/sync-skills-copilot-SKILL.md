---
name: sync-skills
description: Sync the latest Jarvis Registry skills into GitHub Copilot from your Jarvis Registry account.
---

## Arguments

| Variable | Description |
|----------|-------------|
| `$path` | The project directory (absolute, relative, or "."/"this directory"/"current directory") to sync skills into. Required. |

Run `jarvis-registry skills sync $path --mode copilot`, substituting `$path` verbatim (not the
literal text `$path`).

If it fails because the user isn't authenticated, tell them to run `jarvis-registry auth login`
(this is the command that opens a browser device-flow login), then re-run sync. `sync-skills`
itself never opens a browser or starts a login flow; it only ever reads an already-cached
credential and fails loudly if none exists.

If it fails because the destination folder exists but wasn't created by this CLI, tell the user
to run `jarvis-registry skills sync $path --mode copilot` themselves from a real terminal once, to
confirm it's safe for the CLI to manage that folder.

After a successful sync, report the Markdown table from the command's output — which skills were
created, updated, unchanged, or removed — and remind the user they can invoke a synced skill as
`/<skill-name>` (no plugin prefix). If the CLI session is already running, run `/skills reload` to
pick up the newly synced skills.
