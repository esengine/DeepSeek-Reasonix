# Recovery and Safe Mode

Reasonix includes a small recovery executable, `reasonix-guard`, that does not
load Wails, WebView, plugins, MCP servers, hooks, bots, or session transcripts.
It remains usable when the desktop shell or a TOML configuration cannot start.

## Commands

```bash
reasonix-guard check [--root PATH] [--json]
reasonix-guard repair [--root PATH] [--project] [--json]
reasonix-guard diagnose [--root PATH] [--network] [--json]
reasonix-guard rebuild --target tabs|projects|window|zoom|all
reasonix-guard snapshots [--json]
reasonix-guard restore --snapshot ID
reasonix-guard undo [--json]
reasonix-guard launch [--app PATH] [--safe-mode] [--detach]
reasonix-guard recover [--root PATH] [--project]
reasonix-guard assist [--model PROVIDER/MODEL] [--apply] [--allow-project]
reasonix-guard apply-plan --file PLAN.json [--preview-id ID] [--yes] [--allow-project]
reasonix doctor repair [--root PATH] [--apply] [--project] [--json]
```

Windows and Linux packaged desktop shortcuts start through Guard. The macOS
application bundle starts the Wails desktop directly so LaunchServices, the Dock,
and the application window have the same native process identity; it runs the
same startup recovery preflight before creating WebView. Guard remains bundled
as an independent recovery command. Running `reasonix-guard` without a subcommand
launches the sibling desktop executable; use the explicit `check` command for a
read-only configuration check. Windows packages use the same Guard code in a
GUI-subsystem launcher for shortcuts and retain `reasonix-guard.exe` as the
terminal-oriented command.
Windows and Linux shortcuts detach after starting the desktop; an explicit
terminal `reasonix-guard launch` waits by default unless `--detach` is supplied.

`check` and `doctor repair` are read-only unless `repair` or `--apply` is used.
An applied repair renames malformed TOML to a timestamped
`.reasonix-quarantine-*` file. A malformed global config is then restored from
the last-known-good snapshot recorded after a successful desktop startup. The
credential `.env`, session JSONL files, and project source files are never
deleted. Project `reasonix.toml` is only quarantined when `--project` is given.

Guard retains the five newest healthy global-config snapshots. Each snapshot has
a SHA-256 digest and must pass both hash and TOML validation before restore.
Every applied configuration or derived-state repair is persisted in
`last-repair.json` for undo and appended to `repair-log.jsonl` on a best-effort
basis. An audit-log failure never invalidates an otherwise durable repair.
`undo` restores the files moved aside by the latest repair while retaining the
repaired copy as a redo candidate. A multi-action `apply-plan` run is recorded
as one transaction, so a single `undo` reverts the whole plan (or the applied
prefix when a plan failed partway); an interrupted undo resumes from where it
stopped.

`diagnose` adds offline semantic checks for model references, provider and MCP
URLs, credentials, proxy structure, MCP commands, permission conflicts, file
permissions, and derived desktop JSON. `--network` is explicit: it probes the
provider model endpoint through the configured proxy and classifies connectivity
and authentication status without storing response bodies. `rebuild` never
deletes derived state; it quarantines the selected file and lets Reasonix recreate
it.

## Automatic Safe Mode

The desktop records `starting`, `ready`, `healthy`, and `clean-exit` under the
Reasonix state directory. `ready` begins a 30-second probation period. On the
third incomplete startup within five minutes, Guard (or the macOS desktop
preflight) opens a native recovery dialog that does not depend on WebView. Safe
Mode uses built-in configuration, does not restore saved tabs, and disables
external integrations for that run. It does not rewrite the user's configuration.

## Update rollback

Before an automatic **portable** update (Windows installer path, Linux `.tar.gz`,
or a macOS app-bundle replace), Reasonix retains the complete installed release
unit — the desktop executable plus the Guard/launcher binaries the installer
also replaces — or the application bundle (macOS). The backups remain until the
replacement build reaches `healthy` or exits cleanly. If the replacement enters
the startup failure threshold, Guard (or the macOS desktop preflight) verifies
every backup hash and restores the complete release unit before relaunching, so
a rollback never produces a mixed-version install. When the Windows installer
itself fails after the desktop has exited, the update helper records the failure
and relaunches Guard, which performs the same full rollback immediately instead
of waiting for a crash loop. Update metadata and hashes are stored under the
Reasonix repair state; arbitrary backup or target paths are rejected, and
unhashed backups are refused.

**Debian/Ubuntu `.deb` installs** do not use Guard file-level rollback for
upgrades. In-app updates authorize a root helper via Polkit and install with
`apt-get --only-upgrade`. On failure the running process stays up, the verified
download remains cached for retry, and package state is left to apt/dpkg.
Successful installs are managed by the system package manager and are not
auto-downgraded by Reasonix. Users on an older `.deb` without the helper should
overwrite-install the bootstrap package once:
`sudo apt install ./Reasonix-linux-amd64.deb` (no uninstall required).

## Optional AI assistance

Offline `check`, `repair`, `diagnose`, rollback, and Safe Mode never call a model.
`assist` is a separate, explicit second layer. It sends a redacted diagnostic
summary to the selected configured provider as a one-shot request and may incur
provider token charges. It does not change the normal chat prompt or tool list.

The response must be a versioned `RepairPlan` JSON object. Unknown fields and
actions are rejected. The only accepted actions are configuration quarantine,
verified snapshot restore, derived-state rebuild, and pending-update rollback.
The host displays an operation preview and unified configuration diff, asks for
confirmation, and executes only those built-in operations. A plan cannot run a
shell command, edit credentials or session content, or name an arbitrary path.
Machine-readable assist output includes a `planId` and a `previewId`. Hosts
that confirm a dry-run later must pass the same `previewId` to `apply-plan`;
the guard re-computes the preview before writing and refuses when the plan,
action list, filesystem-derived diff, derived-state input, or pending-update
transaction changed. Non-interactive `apply-plan --yes` requires
`--preview-id`; interactive confirmation binds the preview displayed in that
same invocation. Direct package callers that own their own approval boundary
remain source compatible and can opt into the same check with
`ApplyPlanOptions.ExpectedPreviewID`.

Confirmed configuration, snapshot, and derived-state mutations are serialized
across Reasonix processes. Repair transaction bookkeeping and undo are
serialized before per-target locks, so disjoint repairs cannot exchange or lose
undo records. After taking the target lock, Guard re-checks the confirmed action
and its inputs, then verifies the node moved by the final rename. A process that
recreates the target during repair wins: Guard does not overwrite the new file
and reports that the confirmed state was moved aside. Pending-update rollback
uses the complete confirmed transaction identity, not only its version or
timestamp. File updates keep `pending-update.json` immutable and record the
complete installed release unit in a transaction-unique, create-only sidecar,
so a crash cannot hide the rollback transaction between two state-file
replacements.

New fields and sidecars are additive. Older Reasonix releases ignore unknown
data, and current releases can still read legacy transactions with missing
bindings. A missing binding is a safe zero value, not destructive
authorization: automatic handoff, healthy commit, or app-bundle rollback fails
closed when it cannot prove ownership. A legacy app-bundle backup without a
tree digest can be restored only through an explicitly confirmed repair-plan
preview that binds that exact backup.
