# Remote sessions

<a href="../README.md">README</a>
&nbsp;·&nbsp;
<a href="./REMOTE_SESSIONS.zh-CN.md">简体中文</a>
&nbsp;·&nbsp;
<a href="./GUIDE.md">General guide</a>

The remote module (Remote SSH) runs Reasonix on a remote host and reaches it
over your own SSH connection — VS Code Remote-SSH style. This document
describes the whole system: what runs where, host configuration, the CLI, the
remote serve process, the session lifecycle, the desktop surface, credential
modes, and troubleshooting.

The screenshots in this guide use the Simplified Chinese desktop UI; the
controls and states are the same in other locales.

## Contents

- [What the remote module does](#what-the-remote-module-does)
- [What runs where](#what-runs-where)
- [Hosts and configuration](#hosts-and-configuration)
- [Connecting from the CLI](#connecting-from-the-cli)
- [The remote serve process](#the-remote-serve-process)
- [Remote session lifecycle](#remote-session-lifecycle)
- [Desktop remote work](#desktop-remote-work)
- [Credentials and model access](#credentials-and-model-access)
- [Connection behavior and failures](#connection-behavior-and-failures)
- [Troubleshooting](#troubleshooting)
- [Command reference](#command-reference)

## What the remote module does

Reasonix bootstraps a persistent headless `reasonix serve` on the remote host,
forwards a local loopback port to it over the SSH tunnel, and then opens the
serve web client or an in-app remote session tab through that tunnel. The
agent, its tools, and its files all live on the remote host at full fidelity;
nothing runs through a lossy file proxy.

- V1 remote hosts must be Linux or macOS. The local CLI and desktop also run
  on Windows, but V1 Windows authentication does not support the OpenSSH
  named-pipe agent; use an identity file or password instead.
- There is no local background daemon: the CLI's `connect` is a foreground
  supervisor, and the desktop holds its own tunnel.
- Disconnecting the local side never touches the remote serve — it keeps
  running and the next connection reuses it.

## What runs where

```
Local side                                Remote host
──────────                                ──────────
reasonix remote … (CLI)                   ~/.reasonix/remote/
desktop app / separate web window         serve-<slug>.{json,token,port,pid,log}
        │                                          │
        ▼                                          ▼
supervised SSH connection ─── SSH tunnel ─── headless reasonix serve
(keepalive, backoff reconnect,              binds remote 127.0.0.1:0, HTTP + SSE
 TOFU host keys, SFTP)                      agent / tools / files all remote
        │
        ▼  local loopback -L forward
serve web UI in a browser, or the in-app remote session tab
```

- **Local frontends**: the `reasonix remote …` CLI; the desktop app (Wails);
  and serve's own web client (opened in a browser or hosted by the separate
  web-window child process).
- **Transport kernel**: one supervised SSH connection — dial, host-key
  verification, attaching port forwards, keepalive, and backoff reconnect
  after a drop. The CLI and the desktop share the same kernel; interactive
  moments (TOFU confirmation, password/passphrase prompts) surface through
  callbacks to whichever frontend is driving.
- **Remote side**: a headless `reasonix serve` bound only to the remote
  loopback address; port, auth token, and pid are handed over through files,
  never exposed on the remote network.
- **Data plane**: sessions, tool execution, and file operations all happen on
  the remote host; the local side only forwards and renders. Remote file
  browsing and editing go over SFTP, not through serve.

## Hosts and configuration

Hosts live in the user-global `[remote]` section of `config.toml`. Like
`[secrets]`, a project `reasonix.toml` cannot inject or override remote hosts
— a cloned repo can never steer where Reasonix opens SSH connections.

```toml
[remote]
[[remote.hosts]]
name            = "gpu-box"
host            = "203.0.113.7"
user            = "dev"
identity_file   = "~/.ssh/id_ed25519"
workspace       = "~/projects/app"
serve_install   = "auto"            # auto | npm | upload | never
credential_mode = "remote"          # remote | local-proxy

[[remote.hosts.forwards]]
type   = "local"                    # local (-L) | remote (-R)
bind   = "127.0.0.1:5432"
target = "127.0.0.1:5432"
```

### Host fields

| Field | Meaning |
| --- | --- |
| `name` | Host name; CLI subcommands refer to it |
| `host` / `port` / `user` | Address and login user; port defaults to 22, user to the current user |
| `identity_file` | Path to a private key. Only the path is stored; key material is never stored |
| `passphrase_env` / `password_env` | Env var names holding the passphrase/password; values live in Reasonix's global `.env` |
| `proxy_jump` | Jump chain, OpenSSH `ProxyJump` syntax |
| `workspace` | Default remote workspace |
| `serve_install` | Remote CLI install strategy: `auto` \| `npm` \| `upload` \| `never` |
| `credential_mode` | `remote` (key on the remote host) \| `local-proxy` (desktop holds the key); default `remote` |
| `use_ssh_config` | Layer unset fields from `~/.ssh/config` |

`[[remote.hosts.forwards]]` persists port forwards with the host. `type`
selects `local` (`-L`) or `remote` (`-R`). For `-L`, `bind` listens locally
and `target` is dialed from the remote host; for `-R`, `bind` listens on the
remote host and `target` is dialed locally.

`[[remote.projects]]` pins remote workspaces into the desktop project tree:
`host_id` + `workspace` + `title`.

### Credential slots

When the desktop host form receives a plaintext password or key passphrase,
Reasonix stores it in a generated `REASONIX_REMOTE_<hash>_PASSWORD` /
`REASONIX_REMOTE_<hash>_KEY_PASSPHRASE` slot in the global `.env` (atomic
write with rollback on failure) and writes only the slot name to
`config.toml`. Leaving the plaintext field empty preserves the current
reference and does not create a slot. Deleting or clearing the host
garbage-collects unused generated slots; env var names you configured
yourself are never deleted.

### Host resolution precedence

1. Fields set explicitly in `[remote]`;
2. the local `ssh -G` resolution (authoritative; covers `Include`, wildcard
   `Host`, `Match` (including `Match exec`), repeated `IdentityFile`,
   `ProxyJump`, and `IdentitiesOnly`);
3. the built-in `~/.ssh/config` parser;
4. defaults (port 22, current user).

`reasonix remote import` stores the original alias with
`use_ssh_config = true` instead of copying a snapshot that goes stale.

## Connecting from the CLI

### Host management

```bash
reasonix remote add gpu-box dev@203.0.113.7 --workspace '~/projects/app'
reasonix remote import --all        # import aliases from ~/.ssh/config
reasonix remote test gpu-box        # dial + auth + host-key check
reasonix remote list                # list configured hosts
reasonix remote remove gpu-box
```

### connect: the foreground supervisor

`connect` behaves like `ssh -N` plus the serve bootstrap: it establishes and
holds the SSH connection, bootstraps the remote serve, forwards the serve
port to a local loopback port, and attaches the configured forwards. If the
link drops it auto-reconnects with exponential backoff and re-attaches the
forwards. Ctrl-C disconnects the local side only — the remote serve keeps
running, and the next `connect` reuses it.

```bash
reasonix remote connect gpu-box --open   # bootstrap serve, tunnel, open the URL
reasonix remote open gpu-box             # same as connect --open
reasonix remote connect gpu-box --local-port 18787 --no-serve
```

`--no-serve` (alias `--forward-only`) establishes forwards only and does not
bootstrap serve.

For a host with `credential_mode = local-proxy`, use the desktop to bootstrap
and open the workspace. CLI `remote connect` does not create the
desktop-owned reverse credential channel; use `--no-serve` only when you need
the configured forwards without a remote session.

### Remote serve operations

```bash
reasonix remote serve start gpu-box
reasonix remote serve status gpu-box
reasonix remote serve logs gpu-box -n 100
reasonix remote serve stop gpu-box
```

`serve start` refuses hosts with `credential_mode = local-proxy`. The desktop
is required to bootstrap the serve and provide its reverse credential channel.

### Port forwards and remote files

```bash
reasonix remote forward add gpu-box -L 127.0.0.1:5432:127.0.0.1:5432
reasonix remote forward ls gpu-box
reasonix remote forward rm gpu-box 127.0.0.1:5432
reasonix remote fs ls gpu-box:'~/projects/app'
reasonix remote fs get gpu-box:'~/projects/app/main.go' ./main.go
reasonix remote fs put ./patch.diff gpu-box:'~/projects/app/patch.diff'
```

The `fs` subcommands go over SFTP and do not need serve to be running.

## The remote serve process

One serve per workspace: remote state files are named by workspace slug and
never interfere with each other.

**Bootstrap flow** (run automatically by `connect` or when the desktop opens
a remote project):

1. Try to reuse a running serve — it counts as alive only if the pid and the
   launch arguments match exactly, which defeats pid-reuse misjudgment.
2. Probe the remote platform and binary (see the install ladder).
3. Generate a fresh auth token: written to `.token.next` first, then renamed
   atomically, so no reader ever sees a half-written token.
4. Launch `reasonix serve` detached via `setsid`/`nohup`: bound to
   `127.0.0.1:0`, token passed through `--token-file` (never in argv, never
   visible in `ps`), port and pid written to `.port` / `.pid` files.
5. Poll the port file, then write the state JSON and establish the local
   forward.

**Binary install ladder** (tried in order when
`serve_install = "auto"`):

1. an existing Reasonix binary on the remote host;
2. `npm` global install;
3. uploading the local same-platform binary to the remote
   `~/.reasonix/remote/bin/`;
4. downloading from the official release.

Whether a binary is usable is decided by a capability probe, not a version
number: an older binary missing any required serve capability is treated as
missing and upgraded. `serve_install = "never"` forbids all installation.

**Remote state files** (remote `~/.reasonix/remote/`): `serve-<slug>.json`
(pid, bound loopback address, workspace), `serve-<slug>.token` (0600),
`serve-<slug>.port`, `serve-<slug>.pid`, `serve-<slug>.log`.

**Access URL**: `http://127.0.0.1:<local-port>/#token=<token>`. The token
lives in the URL fragment, so it never reaches server logs with a request;
older serve builds fall back to the `?token=` query parameter.

**Stopping**: `serve stop` signals only the process whose pid and launch
arguments match exactly; it never kills an unrelated process.

**Concurrent bootstraps**: clients bootstrapping the same workspace at the
same time are serialized by a remote file lock; the lock expires after 60
seconds of inactivity.

## Remote session lifecycle

- One serve carries one **foreground session**. Switching to another session
  leaves a busy turn running detached in the background until it finishes; it
  is never interrupted.
- A session has a single writer (a lease): while another process holds it,
  resuming that session is refused and the UI reports "session in use".
- **Handoff**: a local window on the serve host may take over the foreground
  session. Serve then degrades to a read-only mirror that forwards the local
  writer's frames in real time; 30 seconds without a writer heartbeat
  reclaims the session automatically, and an explicit reclaim is always
  possible. The desktop remote tab enters spectator mode and shows a reclaim
  banner.
- The desktop project tree lists the workspace's remote sessions. Selecting a
  row resumes that exact session in the shared transcript and composer
  surface; a running turn keeps executing remotely with its state shown in
  the tree. The desktop holds the SSH tunnel and never mixes local
  conversation sessions into the remote tab.

The following screenshots show both ends of a handoff. First, the Reasonix
window running locally on the remote host confirms taking over an idle
session:

![The local window on the remote host confirms taking over an idle session](./assets/remote-session-takeover-idle.png)

After the takeover, the remote-session tab on the connecting desktop becomes
a read-only spectator. It continues receiving the live transcript and offers
a **Take back** action:

![The remote-session tab becomes a read-only spectator and offers Take back](./assets/remote-session-spectator-reclaim.png)

## Desktop remote work

- **Settings -> Remote SSH**: manage hosts — add/edit/remove, scan-import
  from `~/.ssh/config`, connect/disconnect, view status.
- **Add a remote project**: in the project tree's add-project menu choose
  **Remote connection**. The three-step wizard saves or reuses an SSH host,
  connects and verifies that the remote OS is supported, then lets you browse
  and choose a workspace before opening an in-app remote session tab. The
  key-file button uses the native file picker so the saved identity is always
  an absolute desktop path.
- **Remote explorer**: the status-bar chip or the host row's **Remote
  explorer** button — browse and edit remote files over SFTP, manage port
  forwards, start/open the remote workspace.
- **Remote session tab**: the same transcript/composer surface as local
  sessions, with model switching, reasoning effort, plan mode, compaction,
  fork, skills, background jobs, and the other commands; the tab survives a
  brief SSH outage while the desktop reconnects in the background.
- **Model catalog**: in `remote` credential mode it comes straight from the
  remote `/models`; in `local-proxy` mode the desktop-configured catalog is
  shown, filtered to the current provider kind.
- **Dialogs**: TOFU fingerprint confirmation, askpass password/passphrase
  entry, structured connection errors (naming the `known_hosts` file and
  line), and the takeover reclaim banner.
- **Web window**: a separate child process hosts the serve web UI; the login
  ticket is written to a one-shot 0600 file (valid for 2 minutes) instead of
  argv, one instance per host.

### Desktop walkthrough

The project-tree add menu places **Remote connection** beside creating a new
project and opening an existing folder:

![Remote connection in the project-tree add menu](./assets/remote-project-onboarding-menu.png)

The remote connection wizard shows its three stages on the left: connection
configuration, connecting, and choosing a directory. Once SSH is ready, you
can jump to a path, show hidden directories, and choose the workspace to open
in the current window:

![The three-stage remote connection wizard and directory picker](./assets/remote-connect-wizard-directory.png)

After opening, the remote project and its sessions appear in the project tree;
the session keeps the complete transcript, composer, mode and model selectors,
status bar, and session metrics:

![A remote project, its session list, and the complete desktop conversation surface](./assets/remote-session-desktop-overview.webp)

## Credentials and model access

| | `remote` | `local-proxy` |
| --- | --- | --- |
| API key location | the remote host's Reasonix config | the desktop machine |
| Model-call path | remote serve → provider | remote serve → reverse tunnel → desktop key holder → provider |
| Model list source | remote `/models` | desktop-configured catalog (filtered by provider kind) |
| CLI | fully supported | `remote serve start` refuses; `remote connect` cannot provide the desktop-owned credential channel. Use the desktop (`--no-serve` remains valid for ordinary forwards) |

Functional behavior of `local-proxy` mode:

- The desktop injects a managed `[[providers]]` block into the remote
  `config.toml`, pointing at the reverse tunnel address with a scoped token;
  Reasonix maintains that block — do not edit it by hand.
- The credential watchdog polls the reverse tunnel every 3 seconds: a missing
  forward, a failed probe, or port drift triggers a full heal plus a provider
  reload. The tunnel secret necessarily rotates after every SSH reconnect
  (even when the port is unchanged), so a reconnect is always followed by one
  unconditional heal.
- The channel recovers by itself after a brief SSH outage; no manual action
  is needed.

Typed passwords and key passphrases are cached in memory, so reconnects
never re-prompt; a desktop restart requires entering them again.

## Connection behavior and failures

- **Keepalive**: probed every 30 seconds; 3 consecutive misses (10-second
  timeout each) declare the link dead, tear it down, and redial.
- **Reconnect backoff**: full-jitter exponential — starting at 1 s, doubling
  per attempt, capped at 60 s. A transient failure on the first connect is
  reported immediately, never retried silently.
- **Terminal failures**: authentication failures and host-key errors are not
  retried; the desktop marks the remote workspace unavailable until a human
  intervenes. A brief network outage keeps the UI available while the desktop
  reconnects and re-attaches its forwards in the background.
- **Host keys**: verified against your OpenSSH `~/.ssh/known_hosts`
  (read-only) plus the Reasonix-managed `~/.reasonix/remote/known_hosts`. A
  first-seen key prompts for trust-on-first-use and is recorded in the
  managed file; a key that contradicts a recorded one is a hard error naming
  the offending file and line, never auto-accepted.
- **Auth order**: SSH agent → `identity_file` → password / kbd-interactive.
- **Jump hosts**: every `ProxyJump` hop verifies its own host key and
  authenticates with its own credentials; the target host's password is never
  sent to an upstream hop.
- **Forward semantics**: `-L` listeners survive reconnects (connections are
  refused while detached); `-R` listeners are recreated on every reconnect;
  when serve moves ports, the local forward is switched atomically to the new
  address. `remote forward add` warns for a non-loopback bind; a hand-edited
  TOML rule is applied as written without that warning, so review its exposure
  explicitly.
- **SFTP**: handles rotate with each reconnect; remote file operations fail
  during an outage and work again once reconnected.

## Troubleshooting

| Symptom | Cause and remedy |
| --- | --- |
| Host-key conflict; the error names a `known_hosts` line | The remote was reinstalled or its address changed. Verify the line by hand, remove that entry from the named file, and reconnect. Never auto-accepted |
| serve will not start | `serve_install = "never"` with no remote binary, or npm unavailable — switch to `upload` or the release download. Check `remote serve logs` |
| Suspected incompatible older serve | A failed capability probe upgrades automatically; if needed, `remote serve stop` then reconnect to force a fresh bootstrap |
| `connect` stuck bootstrapping | Concurrent bootstraps are serialized by a remote file lock that expires after at most 60 seconds; retry shortly |
| Session reports "in use" | Another process holds the session's lease (another window or serve). Exit from that side or wait for the holder to release |
| Remote tab switched to spectator mode | A local window on the serve host took over the session; it auto-reclaims after 30 s without a heartbeat, or use the reclaim banner |
| `local-proxy` model calls failing | The watchdog heals automatically; confirm the desktop is online and SSH is connected. Never hand-edit the managed remote provider block |
| Authentication failure keeps coming back | Auth failure is terminal and never retried. Check the `.env` slots and key passphrase, or switch to the SSH agent |
| Windows local side | The CLI and desktop are supported, but V1 cannot use the OpenSSH named-pipe agent; configure an identity file or password. Remote hosts must still be Linux/macOS |

## Command reference

| Command | Purpose |
| --- | --- |
| `remote add <name> [user@]host[:port]` | Add a host. Flags: `--identity`, `--jump`, `--workspace`, `--use-ssh-config`, `--serve-install`, `--credential-mode`, `--passphrase-env`, `--password-env` |
| `remote list` | List configured hosts |
| `remote remove <name>` | Remove a host |
| `remote import [alias...]` / `--all` | Import aliases from `~/.ssh/config` |
| `remote test <name\|user@host>` | Dial + auth + host-key check |
| `remote connect <name>` | Foreground supervised connection: bootstrap serve, tunnel, forwards, held until Ctrl-C. Flags: `--workspace`, `--local-port`, `--no-serve`, `--open` |
| `remote open <name>` | `connect --open` |
| `remote status [<name>]` | Without a name, list configured hosts; with a name, print that host's configured target and workspace |
| `remote forward add <host> (-L\|-R) <spec>` | Add a port forward |
| `remote forward rm <host> <bind>` | Remove a forward |
| `remote forward ls <host>` | List forwards |
| `remote serve start\|stop\|status\|logs <name>` | Remote serve lifecycle; `--workspace` selects the workspace, `logs -n` caps lines |
| `remote fs ls <name>:<path>` | List a remote directory |
| `remote fs get <name>:<remote> [local]` | Download a remote file |
| `remote fs put <local> <name>:<remote>` | Upload a file to the remote |

See also: [Configuration paths](./CONFIG_PATHS.md) (where `config.toml` and
`.env` live and how they prioritize) and the [main guide](./GUIDE.md).
