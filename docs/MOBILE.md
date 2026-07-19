# Reasonix Mobile — Product Spec (Frozen Baseline)

This document freezes the product and protocol contracts for the iOS / Android
dual-runtime client. Implementation work tracks this document; breaking changes
require an explicit version bump of the mobile envelope.

## Goals

- One React + Capacitor app for iPhone, iPad, Android phone, and tablet.
- Two **immutable** session runtimes:
  - `local` — phone talks to OpenAI-compatible or Anthropic-compatible providers
    through a lightweight on-device Agent (`mobilecore`).
  - `remote` — phone connects to a Reasonix Node on a computer or server and runs
    the full tool surface (Shell, Git, files, MCP, background jobs).
- Default path needs no account for local providers and LAN / Tailscale direct
  connect. Official relay, cross-network pairing, and push require a Reasonix
  account.
- Minimum OS: iOS 17, Android 10 (API 29). Channels: App Store, Google Play,
  signed APK. Mainland China Android stores are a later release stage.

## Non-goals (v1)

- Local full coding environment (no Shell / Git / arbitrary filesystem / stdio MCP).
- Full end-to-end cloud session sync of conversation content or API keys.
- React Native or separate iOS/Android business stacks.

## Architecture

```
┌──────────────── mobile (Capacitor) ────────────────┐
│  UI  →  SessionBackend  →  LocalBackend | RemoteBackend  │
└───────────────┬───────────────────┬────────────────┘
                │                   │
        mobilecore (Go)      WebSocket MobileEnvelope
        Keychain/Keystore    (LAN / Tailscale / relay)
                │                   │
         Provider APIs        reasonix node (multi-session)
```

### SessionBackend

UI depends only on `SessionBackend`. Runtime is fixed at session creation and
never mutated in place. Migration between local and remote is always
**copy-to-device** or **hand-off-to-node** that creates a new session.

### MobileEnvelope

All mobile transport frames use a versioned envelope:

```ts
MobileEnvelope {
  version: number      // currently 1
  type: string         // command | event | ack | hello | snapshot | error | ping | pong
  requestId?: string   // write commands are deduped by this id
  sessionId?: string
  seq?: number         // monotonic event sequence per session
  ack?: number         // last event seq the peer has applied
  payload?: unknown
}
```

Rules:

- Every write command carries a non-empty `requestId`; the node keeps a bounded
  dedupe table and returns the prior result on retries.
- Every session event carries a monotonic `seq` starting at 1.
- Reconnect sends `lastAckSeq`; valid cursors get incremental replay; expired
  cursors get a full snapshot (history, partial turn, todos, running, pending
  approval, revision).

### SessionDescriptor

Persisted client-side index (and node-side session index):

```ts
SessionDescriptor {
  id: string
  runtime: "local" | "remote"   // immutable after create
  nodeId?: string
  providerRef?: string
  capabilities: string[]
  revision: number
  lastEventSeq: number
  title?: string
  status?: string               // idle | running | pending_approval | failed
  updatedAt?: string            // RFC3339
}
```

New persisted fields always use `omitempty` on the Go side so older clients and
desktop/CLI stores remain readable.

## Local runtime (`mobilecore`)

- Built with `gomobile bind` into Android AAR + iOS XCFramework.
- Exposed to JS via Capacitor plugins (Kotlin / Swift) as a JSON string API.
- Reuses Go provider serialization, reasoning adapters, retry, usage,
  compaction, eventwire, and cache diagnostics. **Do not reimplement provider
  protocol in TypeScript / Kotlin / Swift.**
- Fixed methods: `CreateSession`, `RestoreSession`, `Submit`, `Cancel`,
  `Answer`, `Approve`, `Snapshot`, `SubscribeEvents`, `ListModels`,
  `ProbeProvider`.
- Local tools only: web read, user-authorized attachment read, image input,
  HTTP MCP. No Shell, Git, arbitrary FS, or stdio MCP.
- Tool set and order freeze at session create. Dynamic device state is injected
  as user-turn data only (cache-first).
- API keys in Keychain / Android Keystore. Session snapshots AES-GCM encrypted
  in the app private directory. JS, logs, and crash reports must never persist
  keys.

## Remote runtime (`reasonix node`)

- Multi-session daemon; existing single-session `reasonix serve` is unchanged.
- One WebSocket application protocol for direct connect and official relay.
- Node keeps running when the phone backgrounds, drops network, or is killed.
- One writable runtime owner per session; multiple read-only observers allowed.
- Lease and failure-atomicity rules match desktop/CLI session ownership.

## Account, relay, push

- Cloud stores account, device/node public keys, session index, online status,
  and notification metadata only. Never API keys, code, attachments, or full
  transcripts.
- Relay is ciphertext-only (Noise XX + AEAD). Prefer verified LAN / Tailscale;
  fall back to official relay.
- APNs / FCM carry minimal signals only (done / failed / needs approval).

## Navigation and design

Bottom tabs (exactly four): **Sessions**, **Nodes**, **Providers**, **Settings**.
Chat, approval, Diff, and attachments are detail routes.

Visual system: precision industrial — charcoal / cool-grey surfaces, warm-copper
accent (`#d97757` family aligned with desktop), independent semantic green /
blue / yellow / red. Full dark and light themes. Corner radius ≤ 8px. No nested
cards. Compact tool timeline. Stable monospace for code and Diff.

Motion only for navigation, streaming status, approval feedback, and node
connect (120 / 180 / 260 ms). No decorative gradient orbs, blur backgrounds, or
idle looping animations.

Locales: English, Simplified Chinese, Traditional Chinese. Accessibility:
VoiceOver / TalkBack, WCAG 2.2 AA, 44 / 48 pt targets, reduced motion, high
contrast, non-color status cues.

## Cache-impact policy

Mobile work that touches provider requests, system prompt, tool schemas, or
compaction must include PR footer lines:

```
Cache-impact: ...
Cache-guard: ...
System-prompt-review: ...
```

Local runtime freezes tools at session create and injects dynamic device state
only on user turns so the provider-visible prefix stays stable.

## Compatibility with existing surfaces

| Surface | Contract |
| --- | --- |
| `reasonix serve` | Unchanged HTTP + SSE single-session API |
| Desktop / CLI | Unchanged session file formats; new fields `omitempty` |
| Mobile envelope | Versioned; unknown types are ignored or rejected safely |

## Milestones (summary)

1. Spec freeze, design tokens, protocol schema, `SessionBackend`, cache baseline.
2. Capacitor shell, navigation, session UI, secure storage, `mobilecore`, local chat.
3. `reasonix node`, multi-session hub, WebSocket, snapshot, idempotent commands, LAN pair.
4. Accounts, Noise relay, node management, APNs/FCM, offline reconnect.
5. Attachments, light tools, HTTP MCP, approval, todos, Diff, tablet, three locales.
6. Security audit, fault injection, store assets, beta roll-out, public launch.

## CI (current)

PR / push to `main-v2` runs job **`mobile`** in `.github/workflows/ci.yml`:

- Go: `internal/mobileprotocol`, `internal/mobilecore`, `internal/node`
- React shell: `mobile/` `npm ci`, typecheck, test, Vite production build

Local: `make mobile-ci`. Native iOS/Android packaging, `gomobile bind`, and
store publish are not wired yet.

## Release tags

- Stable: `mobile-vX.Y.Z`
- RC: `mobile-vX.Y.Z-rc.N`
