# Jarvis Registry CLI — Release Notes Style Guide
# This file is read by github/copilot-release-notes@v1 automatically.
# Drop it at .github/release-notes-instructions.md and the action follows these conventions.

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
- **Describe user-visible behavior, not internal mechanics.** Omit transport
  protocols, wire formats, deployment topologies, internal class or module
  names, and other implementation details unless the change directly exposes
  that surface to the user. When in doubt, name the capability (what the user
  can now do) rather than how it is wired underneath.
  This rule applies **even when the PR title or body mentions those details
  explicitly** — many PR descriptions document internal mechanics for
  reviewers, but those details do not belong in user-facing release notes.
  When a feature adds a new entrypoint that fulfills requests by calling
  other services internally, describe the entrypoint and its user-visible
  contract only — not how the request is fulfilled downstream.
- **Verify every specific detail, or drop it.** Only include concrete facts
  (content types, header names, RFC numbers, field names, config keys, env
  vars, file paths, version numbers) if you have read them in the PR's diff,
  body, merge commit, or reviewer notes — not from memory, prior knowledge,
  or inference from surrounding context. If you cannot verify a specific,
  describe the change at a higher level instead. A wrong specific is worse
  than no specific.

## Categories (use these headings in order, omit empty ones)
1. ⚠️ Breaking Changes & Upgrade Notes  ← always first if present
2. ✨ Features
3. 🐛 Bug Fixes
4. 🔧 Refactoring & Performance
5. 📦 Dependencies
6. 🌍 Documentation

A renamed or removed subcommand or flag, or a changed (renamed, removed, or
re-interpreted) config key, counts as a ⚠️ Breaking Change: users' scripts and config
files stop working without edits. State what to change when upgrading.

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
Omit these from the output entirely. Evaluate these rules from the PR title and
labels first — do **not** inspect the diff just to apply a skip rule.
- Pure whitespace / formatting commits
- Documentation-only PRs: title starts with `docs:` or `docs(<scope>):`, OR the
  PR carries a `documentation` label, OR every changed path is `*.md` (only check
  paths if title/labels are inconclusive).
  **Exception:** `skills/embedded/*.md` files are embedded into the CLI binary and
  delivered to users' synced skills directories, so changes to them are
  user-visible — never treat them as documentation for this rule.
- i18n translation-only commits unless they add a new language

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
  `git show --stat <sha>`, `git show -- <single-file> <sha>`.
- Avoid full diffs of large changes. If `git show --stat` reports a PR with
  more than ~500 changed lines, stick to the stat + commit message and the
  changed file list; do not request the full patch.
- If after one or two `git` calls you still cannot confidently describe the
  change, flag it under **🔍 Needs Review** rather than guessing.

## Uncertainty flagging
If Copilot is not confident what a PR changes (e.g. the PR body is empty or the diff
is ambiguous), place the entry under a separate section:

### 🔍 Needs Review
- [ ] PR #NNN — {title} — *could not determine impact; please review manually*

## Release summary
**Required.** Begin every set of notes with a 2–4 sentence plain-English paragraph
summarising what this release is about, placed **before any category heading or
bullet list**. Focus on the most impactful changes, not an exhaustive list. Write the
opening sentence so it stands on its own as a summary — it may be read out of context.
