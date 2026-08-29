# jarvis-registry-cli

Companion CLI for the [Jarvis Registry](https://github.com/ascending-llc/jarvis-registry) platform.

## Commands (kong)

Command structs use `github.com/alecthomas/kong`, which drives them through a three-phase DI lifecycle:

1. **`BeforeReset`** — set defaults that don't depend on parsed flags.
2. **`AfterApply`** — derive fields that depend on parsed flags or on data loaded at startup (e.g. config).
3. **`Run`** — assumes all dependencies are already set; if a dependency needs something only known at `Run` time, resolve it as the first step of `Run`.

`skills.SyncCommand` is the reference implementation. Fields that need to be swapped out in tests (e.g. `configLoadFunc`, `tp`) are exactly the ones with no CLI-flag dependency — inject them as interface-typed or func-typed struct fields, not package-level vars.

To add a new subcommand: define a command type (e.g. `mypkg.MyCommand`) implementing the phases above, then register it as a new field with a `cmd:"" name:"<subcommand-name>"` kong tag on the `cli` struct in `cmd/jarvis-registry/main.go`.

## Config and auth

- New config fields belong on `cfg.Config` with a `mapstructure` tag. Validate and resolve them inside `cfg.Load` the way `Local.Dest`/`Registry.BaseUrl` already are — fail loudly with a path-qualified error, don't silently coerce.
- Credentials go through `creds.KeyringReadWriter` into the OS keyring — never written to disk or logged. New commands needing Registry access should reuse `auth.NewRegistryTokenResolver` (device grant + refresh + keyring caching already implemented) rather than reimplementing token retrieval.

## Package layout and naming

- No folders that exist purely for organization. Every top-level package owns real behavior; `internal/http` is `internal` to keep an implementation detail (a shared, tuned `http.Client`) out of the public API, not to "organize" anything.
- Exported names read naturally with their package qualifier, no stutter (`skills.SyncCommand`, not `skills.SkillsSyncCommand`). Read a new name back as `pkg.TypeName` before committing to it.

## Composition and interfaces

- Favor composition and small dedicated types for concerns that carry their own complexity or get reused elsewhere, rather than inlining everything into the type that uses them (`skills.SyncCommand` relies on separate `Client`, `ManifestReadWriter`, and `TokenProvider`/`Logger` types instead of doing HTTP, manifest I/O, and auth itself).
- Method count on an orchestrating type is not itself a smell — judge by interface depth instead (Ousterhout's "deep module": a simple external interface hiding non-trivial implementation). `SyncCommand` is an example: kong only ever calls its few exported methods; everything else is private implementation for one cohesive responsibility. Extract a new type when private helpers start spanning *unrelated* responsibilities, not simply because there are a lot of them.
- Interfaces are defined at the consumer for DI/testability, not exported from the producer's package.

## Testing strategy

- Start every command with end-to-end test cases before smaller unit tests: drive the real `Run` method against a real `httptest.Server` and a real temp filesystem, with hand-written stubs (not `testify`-generated mocks) for the few things that must be faked. `skills/skills_test.go` is the reference example.
- Prefer `testdata/` fixture files over inline literals for inputs and expected outputs.
- Once an end-to-end baseline exists, add targeted cases for each subsequent change rather than re-deriving the whole flow.
- For functional code with few side effects — input in, output out — use comprehensive table-driven and/or testdata-driven coverage. `cfg/config_test.go` is the reference example.
- Cover happy paths thoroughly, but don't chase a coverage percentage, and don't stub every interface just to force error branches to execute.

## Local dev tooling (required)

- Run `pre-commit install -t pre-commit` before contributing.
- Use the `Makefile` targets rather than calling `go test` / `golangci-lint` / etc. directly — `make all` (test + lint) is the default target and the bar for a change being done.
- `golangci-lint` must pass. Suppress a false positive case-by-case in `.golangci.yaml` under `linters.exclusions.rules`, scoped to the specific `path` and `text`, with a comment explaining why — never a blanket `//nolint` or a disabled linter.
- `tartufo` does secret scanning; exclude false positives via `tartufo.toml`, not by skipping the hook.
- Import grouping and other style rules are auto-fixed by the lint/format tooling — run `make fmt` (or let pre-commit do it) rather than hand-arranging imports.

## Error handling

Use `fmt.Errorf("...: %s", err.Error())` for context-only wrapping — this is the default. Use `%w` conservatively: wrapping with `%w` is a commitment to that error being part of the package's public interface. Reach for it specifically when callers need to distinguish *what kind* of failure occurred, not just *whether* one did — define an exported sentinel named `Err*` and wrap it with `%w`. If the underlying cause is itself a sentinel from another package, don't propagate that one directly; wrap your own `Err*` around it instead (see `creds.ErrCredentialsNotExist` wrapping `keyring.ErrNotFound`), so callers checking `errors.Is` against our API don't break if the underlying library changes. Don't mint a new sentinel for every fallible call — only for failure modes a caller would plausibly branch on.

## Doc comments

All new or changed exported identifiers require godoc-style comments.
