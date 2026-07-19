# Reasonix Mobile

React + Capacitor client for dual immutable session runtimes:

- **local** — on-device lightweight agent via `mobilecore` (gomobile)
- **remote** — WebSocket client to `reasonix node`

Product and protocol contracts: [`docs/MOBILE.md`](../docs/MOBILE.md).

## Layout

```
mobile/
  src/
    protocol/     # MobileEnvelope + SessionDescriptor (mirrors Go)
    backend/      # SessionBackend, LocalBackend, RemoteBackend
    design/       # brand + platform tokens (ios|android|web)
    components/   # TopBar, TabBar, lists, chat, sheet, settings
    i18n/         # en / zh / zh-TW
    App.tsx       # four-tab shell, new-session sheet, dual runtime
  capacitor.config.ts
```

## UI notes

- Brand: industrial charcoal + warm copper; shared across platforms.
- Chrome adapts via `data-platform` (auto-detect, overridable in Settings).
- iOS: large titles, inset grouped lists, 44pt targets.
- Android/Web: material-height bars, full-bleed lists, FAB for new session.
- New session sheet: pick local vs node, then create (remote hits `reasonix node`).
- Motion: page enter, sheet open/close, message appear, stream pulse (respects reduced-motion).
- Node pairing: QR demo + paste `reasonix://node-pair?…` / URL; fingerprint stored locally.
- Approval bottom sheet: risk badge, command/diff, long-press allow for dangerous writes, one-tap deny.

### Try approval demo

In a local session send: `delete tmp/log` or `write_file example` — the approval sheet opens.

## Develop

```bash
cd mobile
npm install
npm run dev    # http://localhost:5174
npm test
npm run build
```

## CI

GitHub Actions job `mobile` in `.github/workflows/ci.yml` runs on every PR to
`main-v2`:

1. `gofmt` / `go vet` / `go test` for `internal/mobileprotocol`, `mobilecore`, `node`
2. `npm ci` + `typecheck` + `test` + production `build` under `mobile/`

Local mirror:

```bash
make mobile-ci
```

Not yet in CI: Capacitor native projects, `gomobile bind`, store release
(`mobile-vX.Y.Z`).

## Node daemon (remote)

```bash
# from repo root
go run ./cmd/reasonix node --addr 127.0.0.1:8790
```

Mobile remote backend posts JSON envelopes to `POST /mobile/command` and will
use `ws://host/mobile/ws` once the Capacitor shell is packaged.

## Native / store (later milestones)

- `npx cap add ios` / `npx cap add android`
- gomobile bind `./internal/mobilecore` → AAR + XCFramework
- Secure storage plugin (Keychain / Keystore)
- Minimum: iOS 17, Android 10 / API 29

## Cache policy

Do not reimplement provider protocols in TypeScript. Dynamic device state is
user-turn data only. Tool order freezes at local session create.
