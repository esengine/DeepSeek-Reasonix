# Remote Workbench + Local Provider Broker

This document describes the Remote Workbench architecture introduced for Issue #6714.

## Goals

- Full desktop workbench projected over SSH onto a remote workspace.
- **Provider configuration and API keys stay on the Desktop.** The Host never needs `DEEPSEEK_API_KEY`.
- One visible Target in the main window, with **Local + one Remote** adapters allowed to run in the background.
- Bidirectional JSON-RPC over SSH stdio (`rpcwire`); no HTTP Gateway and no SSH `-L/-R` broker tunnels.

## Architecture

```
Desktop UI → TargetManager → LocalAdapter | RemoteAdapter
RemoteAdapter → SSH stdio (rpcwire) → attach-workspace proxy → per-workspace remote-runtime
remote-runtime --broker RPC→ Desktop Provider Broker → local Provider / API key
```

## Protocol

- Source of truth: `internal/remote/protocol` Go registry.
- Artifacts (committed):
  - `internal/remote/protocol/schema.generated.json`
  - `internal/remote/protocol/schema_hash.generated.go`
  - `desktop/frontend/src/generated/remoteProtocol.generated.ts`
- Regenerate: `go run ./cmd/remote-protocol-gen -root .`
- Check: `go run ./cmd/remote-protocol-gen -check -root .`
- Handshake compares the complete **Build ID** (`productVersion`, source revision, protocol version, and Schema Hash). Any mismatch is rejected before the Provider Broker is activated. V1 does not auto-install or auto-upgrade the Host CLI.

## Provider Broker

Host → Desktop requests:

- `broker/catalog`
- `broker/stream/open`
- `broker/stream/cancel`

Desktop → Host notifications:

- `broker/stream/chunk`
- `broker/stream/end`
- `broker/catalog-changed`

Catalog entries are non-secret and include `toolCallReasoning` / `warnOnMissingToolCallReasoning` so DeepSeek tool-call reasoning matches local mode.

## TargetManager

- Permanent Local adapter.
- At most one Remote adapter.
- One `activeTarget` projection; hidden Target events use badge/Toast only (no global modal for hidden approvals).
- Desktop restart always opens Local and only shows a **Reconnect** hint for the last Remote (no auto SSH / AskPass / trust).

## Workspace selection

One-click Remote connections select the Host workspace in this order:

1. Last successfully opened workspace for that Host.
2. The Host's configured default workspace.
3. If neither exists, require the user to choose a workspace explicitly in Remote → Server.

`~` and `~/...` are expanded against the remote SSH user's home directory. Reasonix never silently falls back to `/`; users may still choose `/` explicitly, but doing so lets the workbench browse everything that SSH user can read. Configure a project directory as the default workspace. Normal SSH permissions and tool approval rules still apply.

## Explicit non-goals (this integration)

- Host-held Provider credentials
- systemd permanent remote daemon
- Remote child windows, HTTP Gateway, SFTP as workbench data plane
- Full AppBindings parity beyond the workbench RuntimeAPI surface
- Multiple simultaneous Remote hosts
- Remote fork into a second Desktop tab, mirror upload/restore, and direct Git branch mutation

## SSH transports

| Platform | Transport |
| --- | --- |
| Windows | System `ssh.exe`, AskPass helper, Job Object fail-closed process tree |
| macOS / Linux | Go SSH client; remote command `reasonix remote attach-workspace --stdio` |

Remote argv is fixed (`reasonix remote attach-workspace --stdio`). Workspace is passed via initialize DTO / env, never free-form shell interpolation of untrusted paths beyond quoting.

## Attach + runtime lifecycle

1. Desktop opens SSH stdio → `attach-workspace --stdio`.
2. Attach validates `remote/initialize` (Schema Hash hard gate).
3. Attach starts or reuses per-workspace runtime on a private Unix socket and proxies bytes.
4. Unexpected detach: runtime grace **5 minutes**, then snapshot and exit when idle.
5. Explicit disconnect: immediate when not busy; busy remote refuses host swap.

## Provider Trust

Durable store: `<Reasonix home>/remote-provider-trust.json`
Key: `HostID + fingerprint SHA-256` → allowed provider refs.
Never stores API keys, base URLs, headers, env names, or passwords.
New provider refs require re-confirmation; catalog-changed is only sent after re-auth.

## File containment

Clients send relative refs only. Host resolves under workspace and rejects `..`, absolute paths, and symlink leaves. Runtime file APIs are read-only; agent tool writes still execute on the Host under the normal approval policy.

## Mirror

Completed snapshots pull a digest-checked, read-only `session.jsonl` copy under `remote-mirrors/`. Upload/restore is intentionally not exposed in V1.

## Real acceptance matrix (Windows Desktop → Linux Host)

Record SHA evidence before merge:

1. Host has no `DEEPSEEK_API_KEY` / no local Provider config.
2. First Host Key, password/passphrase, Provider trust dialog.
3. Preinstalled Host CLI with an exact matching Build ID; mismatch fails closed before Broker activation.
4. Local DeepSeek chat + tool-call loop via Broker.
5. History reasoning/tool cards; model/effort switch rebuild.
6. File list/search/preview, agent tool writes, Git status/history/commit detail.
7. Local + Remote concurrent; hidden target badge/Toast only.
8. Mid-stream disconnect: no tool replay; recovery record; reconnect continues.
9. Build/schema mismatch fails closed without sending Provider credentials or activating Broker traffic.
10. Mirror pull and digest verification.
11. Clean Desktop exit / force-kill: no orphan ssh/AskPass; remote runtime exits within 5 minutes.

The opt-in live Broker check complements, but does not replace, the physical
Windows-to-Linux matrix. It loads the real Desktop Provider, isolates the Host
home and Provider environment, and sends one bounded model turn through the
production Broker and runtime:

```sh
cd desktop
REASONIX_HOME="$HOME/.reasonix" REASONIX_REMOTE_WORKBENCH_LIVE=1 \
  go test -tags 'live reasonix_remote_integration' \
  -run '^TestRemoteWorkbenchLiveDesktopBroker$' -count=1 -v .
```

Use `REASONIX_REMOTE_WORKBENCH_LIVE_PROVIDER_REF` to select a specific
authorized DeepSeek model reference. The test never logs Provider credentials
or response content.

## Repository co-contributors (source PRs)

This integration records the following source authors as **GitHub repository co-contributors** via commit-level `Co-authored-by` trailers (PR body credits alone are not enough for contribution attribution):

| Source PR | Author | Contribution adopted here |
| --- | --- | --- |
| #6722 | @SivanCola | Local Provider Broker direction, keys stay on Desktop, provider trust intent, checkpoint/mirror direction; Author of integration commits |
| #6725 | @taibai233 | `rpcwire`, generative RuntimeAPI schema, Windows AskPass/Job Object, target fencing ideas; `Co-authored-by` on the ported commits |

Note: only public GitHub noreply emails in commit trailers create effective co-author attribution on GitHub.

## Provenance

| Source | Adopted / adapted |
| --- | --- |
| PR #6722 (@SivanCola) | Local Provider Broker idea, trust model, remote runtime without host keys, checkpoint/mirror direction |
| PR #6725 (@taibai233) | `rpcwire`, generative protocol schema, RuntimeAPI registry approach, Windows AskPass/Job Object direction, target fencing ideas |
| Not adopted | #6725 Host provider credentials, systemd daemon, and the full 71-method Host implementation as-is; strict Build ID failure was adopted |
| Not retained from #6722 | Remote sub-windows, HTTP Gateway, SSH port-forward Broker, SFTP workbench I/O |

## Cache impact

Broker stream open carries structured `provider.Request` bytes. Desktop reuses the local Provider path so tool order, reasoning replay, message ordering, and compaction stay identical to Local. Host/Target/Schema metadata never enter the system prompt or tool schemas.
