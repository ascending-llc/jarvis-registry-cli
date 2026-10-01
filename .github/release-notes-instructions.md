# Jarvis Registry CLI — Release Notes Style Guide

<!-- Read automatically by github/copilot-release-notes@v1 from this path. -->

## Product context
The Jarvis Registry CLI (`jarvis-registry`, with `jr` as a shorthand) is the companion
CLI for Jarvis Registry. It syncs skills from Jarvis Registry onto local disk in
`claude`, `codex`, and `copilot` modes, each targeting that ecosystem's own skills
directory. It is distributed via Homebrew, install scripts, and signed Windows binaries.
Readers are developers and IT admins who install and run the CLI. They care about new
or changed commands and flags, config keys, install/upgrade steps, and platform
support — not marketing language.

## Tone
- Direct and factual. One sentence per bullet explaining the WHAT and WHY.
- No filler phrases: "exciting", "powerful", "seamless", "leverage", "utilize".
- Use active voice. Prefer "Adds X" over "X has been added".
- **Describe user-visible behavior, not internal mechanics.** Omit internal
  Go package, type, or function names, Registry API endpoints, and other
  implementation details unless the change directly exposes that surface to
  the user. When in doubt, name the capability (what the user can now do)
  rather than how it is wired underneath.
  This rule applies **even when the PR title or body mentions those details
  explicitly** — many PR descriptions document internal mechanics for
  reviewers, but those details do not belong in user-facing release notes.
- **Verify every specific detail, or drop it.** Only include concrete facts
  (command and flag names, config keys, env vars, file paths, supported
  platforms, version numbers) if you have read them in the PR's diff,
  body, merge commit, or reviewer notes — not from memory, prior knowledge,
  or inference from surrounding context. If you cannot verify a specific,
  describe the change at a higher level instead. A wrong specific is worse
  than no specific.

## Categories (use these headings in order, omit empty ones)
1. ⚠️ Breaking Changes & Upgrade Notes  ← always first if present
2. ✨ Features
3. 🐛 Bug Fixes
4. 🔧 Refactoring & Performance
5. 🌍 Documentation

A renamed or removed subcommand or flag, or a changed (renamed, removed, or
re-interpreted) config key, counts as a ⚠️ Breaking Change: users' scripts and config
files stop working without edits. So does dropping support for an OS or CPU
architecture: users on it can no longer install or upgrade. State what to change
when upgrading.

A PR that changes only user-facing guides (`docs/**`, `README.md`) goes under
🌍 Documentation. `skills/embedded/*.md` files are not documentation: they are
embedded into the CLI binary and delivered to users' synced skills directories.
Describe changes to them by their user-visible effect, under the matching
category rather than 🌍 Documentation.

## Ordering within each section

List entries in **chronological order: oldest PR first, newest PR last.** The
underlying `git log` returns merges in reverse-chronological order, so you must
explicitly reverse them when writing. Ordering by PR number is a good proxy
when merge timestamps are unavailable.

## Attribution
The action's renderer automatically appends `(#NNN)` to every bullet. Do
**not** include the PR number in your `description` text — if you do, the
final output will show `(#NNN) (#NNN)`. Just write the description; the
action handles attribution.

## Skip rules
Omit these from the output entirely. Decide from the PR title, commit message,
and changed paths (`git show --stat`) — do **not** inspect the diff just to apply
a skip rule.
- Pure whitespace / formatting PRs, as stated by the title or commit message
- Dependency-only PRs (Go module or Go toolchain version bumps), unless the PR
  states a user-visible fix or a platform-support change. Describe those by that
  effect under the matching category.

## Investigating PRs (shell tool usage)

When a PR title and body are not enough to write a useful entry, you may use the
allowed `git` tool to inspect commits — but observe these rules:

- Issue **one** `git <subcommand>` invocation per tool call. Do **not** wrap
  commands in bash constructs (no `for`/`while` loops, no `&&`, `;`, `|`,
  `$(...)`, subshells, or `xargs`). Only bare `git ...` invocations are
  permitted by the runner.
- If you need data about multiple commits, call `git` multiple times — once
  per commit — instead of looping.
- Prefer cheap commands first: `git log -1 --format=%B <sha>`,
  `git show --stat <sha>`, `git show <sha> -- <single-file>`.
- Avoid full diffs of large changes. If `git show --stat` reports a PR with
  more than ~500 changed lines, stick to the stat + commit message and the
  changed file list; do not request the full patch.
- If after one or two `git` calls you still cannot confidently describe the
  change, flag it as uncertain rather than guessing at specifics.
