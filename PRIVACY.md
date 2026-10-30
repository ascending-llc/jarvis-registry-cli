# Privacy Policy

Last updated: October 30, 2026

This policy describes how the Jarvis Registry CLI (`jarvis-registry`, "the CLI"), published by
ASCENDING Inc. ("ASCENDING", "we"), handles information. It covers the CLI only. It does not cover
a Jarvis Registry server, which your organization deploys and operates, or any website or other
ASCENDING product.

## Summary

- The CLI has no telemetry, analytics, crash reporting, or advertising, and does not send
  information to ASCENDING.
- The CLI has no built-in server. It communicates only with the Jarvis Registry server you
  configure, and with GitHub when you update the CLI.
- Credentials are stored on your device and sent only to the Registry server that issued them.

## Information the CLI sends

### To your organization's Jarvis Registry server

The CLI contacts only the Registry URL you set with `jarvis-registry configure` (and the separate
authentication URL, if you set one), and only when you run a command that needs it:

- **`auth login`** starts an OAuth 2.0 device authorization flow. The CLI sends its client ID and
  the requested access scopes, and receives access and refresh tokens. You sign in through your
  web browser, directly with your organization's Registry or identity provider; the CLI never sees
  your password.
- **Token refresh** sends the cached refresh token to obtain a new access token when the current
  one expires.
- **`skills sync`** sends your access token to list the skills you are authorized to use and to
  download their content.

Your Registry server, as with any network request, also receives standard connection metadata
such as your IP address and the HTTP client's user agent. What that server records and how it uses
information is governed by the organization that operates it, not by this policy. Contact your
organization's administrator with questions about its data practices.

### To GitHub

`jarvis-registry update`, and the install scripts in this repository, request release information
from the GitHub API and download release files from GitHub. If the `GITHUB_TOKEN` environment
variable is set, `update` sends it to GitHub to raise the API rate limit. These requests are
subject to the [GitHub Privacy Statement](https://docs.github.com/site-policy/privacy-policies/github-general-privacy-statement).

## Information stored on your device

The CLI stores the following under your user account. It does not upload any of it, except as
described above:

- **Configuration** in `~/.jarvis-registry/config.yaml`, such as your Registry URL and sync
  settings.
- **Credentials** (OAuth access and refresh tokens) in your operating system's credential store
  (macOS Keychain, Windows Credential Manager, or Linux Secret Service). On Linux without a usable
  credential store, or when you enable `local.credentials.file_only`, they are stored instead in
  `~/.jarvis-registry/credentials.json`, readable only by your user account. The CLI never writes
  credentials to its logs or output.
- **Synced skills** and their sync records, in the locations used by the AI coding tools you sync
  to (Claude Code, Codex, or GitHub Copilot) and under `~/.jarvis-registry/`.

`jarvis-registry auth logout` deletes the cached credentials for the configured Registry. It does
not revoke tokens on the server or end your browser sign-in session. Deleting the
`~/.jarvis-registry/` directory and uninstalling the CLI removes the rest of the CLI's local data.
Skills synced into your AI tools' folders remain until you delete them.

## Changes to this policy

We may update this policy as the CLI changes. The current version is always published at
<https://github.com/ascending-llc/jarvis-registry-cli/blob/main/PRIVACY.md>, and its history is
available in this repository's commit log.

## Contact

For questions about this policy or the CLI, open an issue at
<https://github.com/ascending-llc/jarvis-registry-cli/issues/new>.
