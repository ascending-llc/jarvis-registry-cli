# jarvis-registry-cli

Companion CLI for [Jarvis Registry](https://jarvisregistry.com/): sync the AI coding skills your
organization curates in the Registry to Claude Code, Codex, or GitHub Copilot.

## Installation

### macOS (Homebrew)

```sh
brew tap ascending-llc/jarvis
brew install ascending-llc/jarvis/jarvis-registry
```

See [Setup](docs/setup.md) for Windows, Linux, Go toolchain, upgrade, and configuration instructions.

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

## Documentation

- [Setup](docs/setup.md) — install, upgrade, and configure the CLI.
- [Getting started](docs/getting-started.md) — integrate with Claude Code, Codex, or GitHub Copilot.
- [Troubleshooting](docs/troubleshooting.md) — resolve authentication, path, collision, and link issues.

Run `jarvis-registry --help` or `jarvis-registry <command> --help` for the full command and flag
reference.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for the local development and pull request checks.
