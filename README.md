# jarvis-registry-cli

Companion CLI for [Jarvis Registry](https://github.com/ascending-llc/jarvis-registry).

## Install

### Homebrew

```
brew tap ascending-llc/jarvis
brew install ascending-llc/jarvis/jarvis-registry
```

Upgrade with `brew upgrade jarvis-registry`.

### go install

```
go install github.com/ascending-llc/jarvis-registry-cli/cmd/jarvis-registry@latest
```

## Development

Install the pre-commit hooks:

```
pre-commit install -t pre-commit
```

Use `make` for local development tasks (test, lint, fmt, etc.) — see the [Makefile](./Makefile) for all targets.
