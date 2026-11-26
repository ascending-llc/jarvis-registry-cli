# jarvis-registry-cli

Jarvis Registry CLI shares private AI skill packages from [Jarvis Skill Gateway](https://jarvisregistry.com/features/skill-gateway/) with Claude Code, Codex, Cursor and GitHub Copilot. Maintainers can create and edit skills directly in Jarvis Registry, import them from a Git repository, or use both workflows in the same catalog. Developers sign in and sync their accessible skills to their coding tools.

Use this route when different developers or groups need access to individual skills in a private catalog. For a local folder or repository whose existing sharing process already meets your needs, a separate Registry may be unnecessary. Registry controls who can retrieve each package. For Git-backed skills, repository access is managed separately. Permissions to use live tools are also managed separately.

See [sharing one package across Claude Code, Codex and Cursor](#share-one-private-skill-across-claude-code-codex-and-cursor) for the project layouts and checks.

## Installation

### macOS (Homebrew)

```sh
brew tap ascending-llc/jarvis
brew install ascending-llc/jarvis/jarvis-registry
```

See [Setup](docs/setup.md) for Windows, Linux, Go toolchain, upgrade, configuration, and
[shell completion](docs/setup.md#shell-completion) instructions.

## Updating

For Linux and Windows installations made with an install script or a manually downloaded release:

```sh
jarvis-registry update --check
jarvis-registry update
```

Self-update, including `--check`, refuses Homebrew and winget installations. Use
`brew upgrade jarvis-registry` or `winget upgrade Ascending.JarvisRegistryCLI`, respectively.
For `go install` builds, rerun the install command.
See [Updating the CLI](docs/setup.md#updating-the-cli) for first-upgrade and shell completion details.

## Get started

Configure the Registry connection and sign in:

```sh
jarvis-registry configure
jarvis-registry auth login
```

`configure` sets:

- The Registry base URL.
- The default integration mode: `claude`, `codex`, or `copilot`.

Optional settings let you:

- Override the authentication URL for local development.
- Skip specific Registry skill IDs.
- Replace personal Codex or GitHub Copilot link conflicts.

See the [configuration reference](docs/getting-started.md#configuration-reference) for all fields and
how they are managed.

Then choose your integration:

### [Claude Code](docs/getting-started.md#claude-code)

```sh
jarvis-registry skills sync --mode claude
```

This also installs the Registry MCP connection in the Claude Code plugin. Reload the plugin or start
a new session when prompted, then complete the separate Registry MCP OAuth login through `/mcp`.
See the [Claude Code setup guide](docs/getting-started.md#claude-code) for loading and verification.

### [Codex](docs/getting-started.md#codex)

```sh
jarvis-registry skills sync --mode codex
```

### [GitHub Copilot](docs/getting-started.md#github-copilot)

```sh
jarvis-registry skills sync --mode copilot
```

These commands use personal scope, making skills available across projects. Follow the integration
links for project-scope commands, skill locations, invocation names, and advanced sync options.

## Share one private skill across Claude Code, Codex and Cursor

Use the same skill in all three clients. Whether you created the skill in Registry or imported it from Git, `jarvis-registry skills sync` delivers the signed-in developer's accessible, enabled skills to the chosen local layout. The client still has to discover and load those files.

Start with a test Registry and the [two-file release-note sample](https://jarvisregistry.com/walkthroughs/examples/skills/private-skill-samples.zip). Its `release-notes-check` package contains `SKILL.md` and `references/checklist.md`. Prepare revision 1 using either workflow:

- **Create it in Registry.** In the [Registry editor](https://jarvisregistry.com/features/skill-gateway/#manage-skills-in-the-registry), create `release-notes-check` from the sample’s instructions and add `references/checklist.md` as a supporting file.
- **Import it from Git.** Follow the [private-skill lifecycle walkthrough](https://jarvisregistry.com/walkthroughs/private-skill-lifecycle/) to import the sample from a reviewed commit. For the two-account test, keep the Git source private to its owner.

Whichever path you use, record the skill ID and Registry version, enable the skill, and give the test developer Viewer access to the skill.

After [installing and configuring the CLI](docs/setup.md), sign in to that Registry:

```sh
jarvis-registry configure
jarvis-registry auth login
jarvis-registry auth status
```

Use three new, empty projects. Project-scoped sync owns its destination directory and can remove files outside its tracked set. Decline takeover of an existing directory. On macOS or Linux, `skill_test_dir` holds the three temporary projects. Run the commands below in the same shell.

```sh
skill_test_dir="$(mktemp -d)"
mkdir "$skill_test_dir/claude" "$skill_test_dir/codex" "$skill_test_dir/cursor"

jarvis-registry skills sync --mode claude "$skill_test_dir/claude"
jarvis-registry skills sync --mode codex "$skill_test_dir/codex"
jarvis-registry skills sync --mode codex "$skill_test_dir/cursor"
```

Each command syncs the developer's accessible, enabled set, subject to CLI skip settings. It does not select only this sample. Limit the test identity to appropriate sample content.

| Coding client | CLI mode | Sample location relative to that project |
| --- | --- | --- |
| Claude Code | `claude` | `.claude/skills/jarvis-registry/skills/release-notes-check/` |
| Codex | `codex` | `.agents/skills/release-notes-check/` |
| Cursor | `codex` | `.agents/skills/release-notes-check/` |

This example uses the `codex` project layout, `.agents/skills/`, for Cursor. [Cursor also supports `.claude/skills/` compatibility](https://cursor.com/docs/skills); that is a separate route, not an additional sync step here. There is no separate `cursor` CLI mode. These are project-scope examples. Omitting the project path selects personal scope, whose layouts differ.

In each project, inspect both sample files and the `skill-lock.json` manifest. Compare the recorded skill ID and version with the skill in Registry. The CLI reconstructs the frontmatter in `SKILL.md`, so check the skill name, instructions and supporting-file contents against the saved revision.

Open each project in its coding client and explicitly invoke the sample. Claude Code uses `/jarvis-registry:release-notes-check`; Codex can select `release-notes-check` through `/skills` or `$release-notes-check`; Cursor can select it from `/` in Agent chat. Verify the supported client version and policy first. Claude Code's automatic plugin discovery requires [v2.1.157 or later](https://github.com/anthropics/claude-code/releases/tag/v2.1.157); reload the plugin or start a fresh session after updates.

Use the same harmless note in all three clients:

```text
Owner: Test Release Team
Rollback: Restore the previous test configuration.
```

Record discovery and invocation separately from the resulting text. Revision 1 checks those two items. Revision 2 adds a verification-step check. For a Registry-authored skill, update the instructions and supporting file in the editor. For a Git-backed skill, import the second revision from the repository. Record the new version in Registry, repeat the three sync commands in the same projects, inspect both updated files, and invoke the skill again. An answer that sounds plausible by itself does not prove which package loaded.

Finally, compare an allowed developer with an authenticated developer who lacks every effective grant on the skill. Use separate test OS accounts or isolated environments so cached credentials do not mix. After removing the allowed developer's access, verify blocked Registry retrieval and then a successful sync removing the managed copy. Independently saved files and already-loaded client content require separate handling.

The [three-client private-distribution guide](https://blog.ascendingdc.com/blog/distribute-private-skills-claude-code-codex-cursor/) covers this complete coding-client exercise. The [shared-library guide](https://blog.ascendingdc.com/blog/shared-skill-library-claude-codex-cursor/) covers release ownership, adapters, rollback and compatibility. Use the [CLI getting-started reference](docs/getting-started.md) for directory ownership and the remaining personal-scope and Copilot modes.

## Documentation

- [Setup](docs/setup.md) — install, upgrade, and configure the CLI, and enable shell completion.
- [Getting started](docs/getting-started.md) — integrate with Claude Code, Codex, or GitHub Copilot.
- [Troubleshooting](docs/troubleshooting.md) — resolve authentication, path, collision, and link issues.

Run `jarvis-registry --help` or `jarvis-registry <command> --help` for the full command and flag
reference.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for the local development and pull request checks.
