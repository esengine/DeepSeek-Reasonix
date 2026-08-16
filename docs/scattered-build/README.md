# Reasonix Scattered Build

This directory documents a source-built Windows distribution maintained in a
personal fork. It is not an upstream release channel and it does not replace
the upstream project's release or review process.

## Scope

The scattered build contains the same source tree as the selected fork branch,
with reviewed local fixes carried as normal Git commits. Desktop and CLI
artifacts are built from one source commit and one version identity before they
are deployed locally.

This repository stores source and documentation only. It does not store the
compiled desktop application, CLI binaries, user configuration, credentials,
session history, SQLite files, WebView2 data, logs, or machine-specific paths.

## Build contract

1. Check the upstream `main-v2` head without changing the working tree.
2. Rebase or merge new upstream commits only after reviewing conflicts against
   the local fixes.
3. Run focused tests for changed packages, then the required desktop smoke and
   lifecycle checks when desktop code is involved.
4. Build the desktop application and CLI from the same source commit.
5. Record the source commit, build identity, artifact hashes, and update result
   in a local manifest that is not committed with runtime data.
6. Add a dated public update report before pushing the source branch.

The source update check is read-only. A network failure is reported as
`check-failed`; it is never treated as “no update”, and it never fetches,
installs, replaces binaries, or modifies user data by itself.

## Runtime isolation

Isolation is a data-directory boundary for the scattered build, not a virtual
machine, container, proxy, or security boundary. A Windows launch wrapper sets
these process environment variables before starting the binary:

```text
REASONIX_HOME=<scattered-home>
REASONIX_STATE_HOME=<scattered-state>
REASONIX_CACHE_HOME=<scattered-cache>
REASONIX_CREDENTIALS_STORE=file
```

The three directories keep configuration and credentials, sessions and
recovery state, and cache/WebView2 data together under the current scattered
version. A direct executable launch does not inherit variables from a wrapper;
the wrapper or an equivalent environment setup is therefore the supported
entry point.

## Public push reports

Every public push adds one new file under
`docs/scattered-build/updates/` using this form:

```text
YYYY-MM-DD-<version-or-topic>.md
```

Each report records, briefly and separately for every logical commit:

- date;
- problem;
- solution;
- logic and boundary;
- result and verification status.

Reports must distinguish passed checks, blocked checks, and work that was not
performed. They must not include API keys, complete session content, local
usernames, machine-local absolute paths, or raw private configuration.

## Relationship to upstream

The fork can monitor upstream progress and submit pull requests to `main-v2`,
but a fork push is not an upstream merge. A report must never describe a pull
request as merged until the upstream repository shows that state.

## Latest public verification

The 2026-08-15 source-update adapter fix detects a refused loopback proxy and
retries the read-only GitHub check once without proxy variables. The fallback
requires all three signals: a proxy-related error, a loopback address, and an
explicit connection refusal. DNS, TLS, authentication, remote-proxy, and other
network failures remain `check-failed` and do not bypass the configured proxy.
Deterministic branch coverage and a live GitHub check passed. The corresponding
public report is
`updates/2026-08-15-v1.25.2-loopback-proxy-fallback.md`.
