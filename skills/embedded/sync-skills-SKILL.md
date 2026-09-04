---
name: sync-skills
description: Sync the latest Jarvis Registry skills into Claude Code from your Jarvis Registry account.
disable-model-invocation: true
---

Run this exact command:

    jarvis-registry sync-skills

If it fails because the user isn't authenticated, tell them to run `jarvis-registry auth login`
(this is the command that opens a browser device-flow login), then re-run sync. `sync-skills`
itself never opens a browser or starts a login flow; it only ever reads an already-cached
credential and fails loudly if none exists.

If it fails because the destination folder exists but wasn't created by this CLI, tell the user
to run `jarvis-registry sync-skills` themselves from a real terminal once, to confirm it's safe
for the CLI to manage that folder.

After a successful sync, report the Markdown table from the command's output — which skills were
created, updated, unchanged, or removed — and remind the user they can invoke a synced skill as
`/jarvis-registry:<skill-name>`. If the output says this is the first time skills were synced on
this machine, tell the user to start a new Claude Code session once before the new skills appear.
