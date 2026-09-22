---
name: sync-skills
description: Sync the latest Jarvis Registry skills into Codex from the user's Jarvis Registry account.
---

## Arguments

| Variable | Description |
|----------|----------------|
| `$path` | Optional project directory (absolute, relative, or "."/"this directory"/"current directory") to sync skills into. Omit if the user didn't specify one — omitting it syncs into the user's personal, cross-project skill set instead, and nothing is written under any project directory. |

If the user specified a project directory, run `jarvis-registry skills sync --mode codex $path`,
substituting `$path` verbatim (not the literal text `$path`). Otherwise, run
`jarvis-registry skills sync --mode codex` with no path argument at all. Never pass
`-i`/`--interactive` yourself — it prompts on stdin, which you can't answer.

If it fails with `not logged in to the Registry`, first retry the exact same command requesting
escalated/unsandboxed execution before concluding the user needs to log in. Codex's default sandbox
can block macOS Keychain access in a way that reads back as "credential not found" even when the
user is already logged in, so this specific error is not reliable evidence of a missing login until
an unsandboxed retry has ruled that out. Only if the escalated retry fails with the same error should
you tell the user to run `jarvis-registry auth login` (this is the command that opens a browser
device-flow login), then re-run sync. `skills sync` itself never opens a browser or starts a login
flow; it only ever reads an already-cached credential and fails loudly if none exists.

If it fails because the destination folder exists but wasn't created by this CLI, tell the user to
run `jarvis-registry skills sync --mode codex` themselves from a real terminal once (carrying through
`$path`, if the user specified one), to confirm it's safe for the CLI to manage that folder.

When no project directory was given, the sync summary table has a `Link` column reporting whether
each skill was made visible to Codex under `~/.codex/skills/`. Most rows read `Linked`, `Unchanged`,
or `Relinked` and need no action. A row reading `Skipped` means something else already exists at that
name under `~/.codex/skills/` and was left untouched — tell the user their options: remove or move the
conflicting file/folder themselves, set `local.skills.link.override: true` in
`~/.jarvis-registry/config.yaml` to always replace it going forward, or re-run the same command with
`-i`/`--interactive` themselves from a real terminal to decide per skill.

After a successful sync, report the Markdown table from the command's output — which skills were
created, updated, unchanged, or removed — and remind the user they can invoke a synced skill by
its bare name. If the output says this is the first time skills were synced into this folder, a
new session may be needed before the new skills are discovered.
