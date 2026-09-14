---
name: sync-skills
description: Sync the latest Jarvis Registry skills into Codex from your Jarvis Registry account.
---

## Arguments

| Variable | Description |
|----------|-------------|
| `$path` | The project directory (absolute, relative, or "."/"this directory"/"current directory") to sync skills into. Required. |

Run `jarvis-registry skills sync $path --mode codex`, substituting `$path` verbatim (not the
literal text `$path`).

If it fails with `not logged in to the Registry`, first retry the exact same command requesting
escalated/unsandboxed execution before concluding the user needs to log in. Codex's default sandbox
can block macOS Keychain access in a way that reads back as "credential not found" even when the
user is already logged in, so this specific error is not reliable evidence of a missing login until
an unsandboxed retry has ruled that out. Only if the escalated retry fails with the same error should
you tell the user to run `jarvis-registry auth login` (this is the command that opens a browser
device-flow login), then re-run sync. `skills sync` itself never opens a browser or starts a login
flow; it only ever reads an already-cached credential and fails loudly if none exists.

If it fails because the destination folder exists but wasn't created by this CLI, tell the user
to run `jarvis-registry skills sync $path --mode codex` themselves from a real terminal once, to
confirm it's safe for the CLI to manage that folder.

After a successful sync, report the Markdown table from the command's output — which skills were
created, updated, unchanged, or removed — and remind the user they can invoke a synced skill by
its bare name. If the output says this is the first time skills were synced into this folder, a
new session may be needed before the new skills are discovered.
