# Setup

Install the CLI, obtain your Registry URL from your administrator, and configure a default skills
integration before following [Getting started](getting-started.md).

## macOS

Install with Homebrew:

```sh
brew tap ascending-llc/jarvis
brew install ascending-llc/jarvis/jarvis-registry
```

Upgrade with `brew upgrade jarvis-registry`. Bash, zsh, and fish completions are installed
automatically. After a fresh installation, restart your shell before using them.

## Windows

Install with winget:

```powershell
winget install Ascending.JarvisRegistryCLI
```

Upgrade with `winget upgrade Ascending.JarvisRegistryCLI`.

## Linux

The installer supports Linux amd64 (x86_64) and arm64 (aarch64). The default per-user installation
does not require `sudo`. Install the latest release, the `jr` shorthand, and shell completions with:

```sh
curl -fsSL https://raw.githubusercontent.com/ascending-llc/jarvis-registry-cli/main/scripts/install.sh | bash
```

Review the [installer source](../scripts/install.sh) before running it. See [Updating the CLI](#updating-the-cli)
for upgrades. The default binary directory is `~/.local/bin`; the installer prints any
needed `PATH` and zsh completion setup instructions without changing your shell configuration.
Bash completion requires `bash-completion` v2 to be installed and sourced by your shell. Completion
paths honor `XDG_DATA_HOME` (bash/zsh) and `XDG_CONFIG_HOME` (fish); some versions of `bash-completion`
cannot auto-load completions when `XDG_DATA_HOME` contains spaces.

To choose a different directory or a particular release, set variables on the `bash` side of the pipe:

```sh
curl -fsSL https://raw.githubusercontent.com/ascending-llc/jarvis-registry-cli/main/scripts/install.sh |
  BINDIR="$HOME/bin" JARVIS_REGISTRY_VERSION=v0.6.6 bash
```

Replace the example version with the release you want. The installer supports v0.6.4 and later;
install older releases manually from the releases page. Setting `JARVIS_REGISTRY_VERSION` skips the
GitHub latest-release API lookup, so it also works around failures at that step, such as API rate
limiting. It still requires access to the installer URL and GitHub release downloads.

You can also install an archive manually from the
[releases page](https://github.com/ascending-llc/jarvis-registry-cli/releases).

## Go toolchain

If the Go toolchain is installed and `GOBIN` is on your `PATH`:

```sh
go install github.com/ascending-llc/jarvis-registry-cli/cmd/jarvis-registry@latest
```

A CLI installed this way always reports `dev` for `jarvis-registry --version`.

## Updating the CLI

For Linux and Windows installations made with an install script or a manually downloaded release:

```sh
jarvis-registry update --check  # Report an available version without changing the executable.
jarvis-registry update          # Download, verify, and install a newer release.
jarvis-registry --version
```

The command checks releases in `ascending-llc/jarvis-registry-cli` directly on GitHub; Registry
configuration and login are not required. It selects a release for your operating system and
architecture, verifies the download against the release's SHA-256 `checksums.txt`, and replaces
the resolved executable. The `jr` symlink continues to work. A checksum failure leaves the
installed executable untouched. On Windows, self-update does not verify Authenticode signatures.

If the current version is already the latest, the command reports that and exits successfully.
It also refuses to downgrade a newer local version. `--check` exits successfully when a newer
version is available and does not download the archive or change the executable. Network,
validation, and installation failures exit with an error. The executable's directory must be
writable; for an administrator-installed copy, use an appropriately privileged terminal.

Only one self-update can install into a given executable path at a time. Another attempt
reports that an update is already in progress; wait for it to finish before retrying. `--check`
does not acquire this lock. The small `.jarvis-registry.update.lock` file (or
`.jarvis-registry.exe.update.lock` on Windows) stays beside the executable; its presence does
not mean an update is running. The operating system releases the lock even if the updating
process crashes. Do not delete the lock file while an update is running.

Use the upgrade method matching your installation:

- **Homebrew:** `brew upgrade jarvis-registry`. Self-update, including `--check`, refuses to run
  on a Homebrew-managed executable.
- **winget:** `winget upgrade Ascending.JarvisRegistryCLI`. Continue using winget to manage
  these installations; the self-update command does not detect winget ownership.
- **Go toolchain:** rerun `go install github.com/ascending-llc/jarvis-registry-cli/cmd/jarvis-registry@latest`.
  These builds report `dev`, so both self-update and `--check` refuse to run.

**First upgrade:** a version released before `update` was introduced cannot run this command.
Use your original installer or download a current release once; subsequent upgrades can use
`jarvis-registry update`.

**Shell completions:** self-update replaces only the executable. To refresh completion files
previously copied into your shell's configuration directories, rerun the Linux installer or
copy the completion files from the new release archive, then restart your shell.

GitHub access is required. If GitHub reports a rate limit, wait before checking again; avoid
running `--check` frequently in shell startup scripts or scheduled jobs.

## Configure the CLI

Run the interactive configuration command:

```sh
jarvis-registry configure
```

Enter the Registry base URL supplied by your administrator and choose `claude`, `codex`, or
`copilot` as the default skills sync mode. The CLI writes these values to
`~/.jarvis-registry/config.yaml`; rerun `configure` whenever they need to change.

Confirm the installation and review available commands:

```sh
jarvis-registry --version
jarvis-registry --help
```

Continue with [Getting started](getting-started.md) to sign in and sync skills.
