# ops-events

Maintainer-only event relay. GitHub webhooks and our own workers push compact
metadata events to this worker; one local listener holds a WebSocket and is woken
only when something happens, instead of polling.

- `GET /ws?since=auto|<id>&token=...` WebSocket, one JSON text frame per event (replays what no listener has received yet when `since` is `auto`). At most 4 listeners.
- `POST /github` GitHub webhook (`X-Hub-Signature-256` is verified before the body is parsed; deliveries are deduplicated). Mapped events: issues opened/reopened/closed, comments (not bots or the owner), pull requests (opened, reopened, synchronize, ready, merged, closed), reviews, the `CI` workflow result, discussions, releases.
- `POST /emit` bearer `OPS_EMIT_TOKEN`: `{t, src, n?, title?, by?, url?, extra?}` (60 per minute).
- Events never contain issue, comment or feedback bodies; the only free text is a title (120 characters, control characters removed).

Secrets (repo secrets mirrored by `deploy-ops-events.yml`): `OPS_GITHUB_WEBHOOK_SECRET`, `OPS_LISTEN_TOKEN`, `OPS_EMIT_TOKEN`. The listen token appears in the URL of the stream; it only grants read access to compact metadata and is rotated by setting a new repo secret and redeploying.

GitHub webhook: payload URL `https://<worker>/github`, content type `application/json`, secret `OPS_GITHUB_WEBHOOK_SECRET`, events: Issues, Issue comments, Pull requests, Pull request reviews, Pull request review comments, Workflow runs, Discussions, Discussion comments, Releases.
