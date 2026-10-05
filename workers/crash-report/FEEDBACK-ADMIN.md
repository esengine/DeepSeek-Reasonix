# Feedback administration

## Trusted feedback limits

Trust is derived only from server records, never from submission fields. Active
trusted installs may submit 12/hour and 60/day and reply 10/hour; untrusted
installs retain 3/hour, 10/day and 3 replies/hour. The 10 user replies per report
cap is unchanged. Trusted installs bypass both the IP binding and D1 IP quota
because unrelated installs can share a NAT. Install/IP blocks and global daily
and burst ceilings still apply. Untrusted submissions stop at 90% of the daily
global budget; trusted submissions may consume the remaining share.

An explicit maintainer release grants 30 days from release, or 90 days after
five distinct released receipts. Automatic received status alone does not count.
Renewals never shorten an existing expiry. Reject and takedown revoke trust;
automatic and explicit blocks always override grants.

The external CLI can map `trust <receipt>` to authenticated
`POST /v1/admin/feedback/<receipt>/trust` (empty body), and `untrust <receipt>` to
`DELETE` on that path. POST grants 365 days; DELETE revokes. Both commit an audit
entry with the mutation. Neither changes blocks. The CLI itself and browser
controls are outside this change. Missing receipts return 404, unsupported
methods 405, and all routes use the existing admin bearer/lockout gate.

## Feedback levels

An install earns one credit when a maintainer-authenticated `POST .../status`
moves its report to `fixed` with a concrete published version (`vX.Y.Z`), either
directly or from `"next"`. The credit commits in the same transaction as the status
change, the trust renewal (30 days, never revoked or blocked installs) and an
`adopt` audit row. The item is the issue the report is linked to
(`owner/repo#number`, taken from the stored issue, never from the request) and is
credited once across all installs; replays, retries, later patch versions and
duplicate reporters add nothing. Submissions, replies, releases, `next`, `wontfix`
and `duplicate` never credit.

Level is the largest threshold not above the active credit count; the only table is
`FEEDBACK_LEVELS` in `src/feedback_level.ts`. L0 is the untrusted tier (3/10/3) and
L6 the shipped trusted tier (12/60/10). Quotas follow live trust: trust without
credit is `legacy_active` and keeps 12/60/10, but only while the install has never held a ledger row (a tombstoned credit never raises limits); with credit the level row applies;
a lapsed or revoked install keeps its level and count but is admitted at L0.
IP-limiter exemption, reserved budget share and the held-queue bypass follow live
trust as before; blocks are unchanged. `GET /v1/feedback/mine` adds a read-only
`profile` object derived from the authenticated install.

Administration, all behind the admin bearer:

- `POST /v1/admin/feedback/<receipt>/adoption` with `{"version":"vX.Y.Z"}` records a
  fixed report that predates the ledger, or an independent outcome of a duplicate
  reporter. Idempotent.
- `DELETE /v1/admin/feedback/<receipt>/adoption` tombstones the credit: the row
  stays, leaves the count, and cannot be credited again (`feedback.adoption_tombstoned`).
  This is the only way a level drops. The high-water mark never decreases.
- Revoking trust (`DELETE .../trust`, reject, takedown) marks the install revoked, so
  later credits count but do not renew trust. An explicit trust grant or a release
  clears the mark.

## Refusal contract

Every feedback 429 carries `error.params` with exactly `limit`, `resetsAt`
(UTC ISO timestamp or null), and `retryAfterSeconds` (integer or null).
`Retry-After` matches the integer when a wait applies. Limit identifiers are
`ip_hourly`, `install_hourly`, `install_daily`, `reply_hourly`, `reply_item`, and
`admin_attempts`. The permanent item cap uses null reset/retry values and no
Retry-After. Existing `feedback.rate_limited` and `feedback.reply_limit` codes
remain. Global refusals retain HTTP 503/`feedback.busy` and include the same
params shape with `global_daily` or `global_burst`.

Hourly/daily limits reset on UTC boundaries; admin attempts use 15-minute
windows. The Cloudflare IP binding does not expose its reset time: public IP
refusals conservatively use the ordinary hourly IP boundary, for blocked and
ordinary callers alike. Global burst refuses for a conservative 60 seconds.
These are retry hints, not promises of admission.

Blocked responses use the same admission decision as ordinary callers: IP
binding first, then submission burst budget, caller counters and daily budget;
replies check hourly quota before ownership/status and the per-report cap.
Only if these gates allow admission is a block concealed behind the first
ordinary hourly window (IP for untrusted, install/reply for trusted). No block
status or expiry is included in body or headers; temporary and permanent blocks
use the same code path. Invalid attachments and reply bodies are validated
before admission for every caller. Submission replay still recovers the
original receipt/token before new-admission gates. Late concurrent replays and
failed storage refund their D1 admission counters; external limiter bindings
cannot be refunded. Refunds are not crash-atomic with report persistence.

New submissions require the install token when any report, trust grant or release
ledger entry identifies the install, including after report retention. A release
entry alone proves prior registration, not active trust. Existing receipt/key
replays still recover a lost token. When Turnstile is enabled, new submissions
must pass the same challenge gate before either ordinary or blocked admission;
blocked callers also perform verification when supplying a challenge token.

## Deployment and client follow-up

A schema addition is required in `migrate-feedback-triage.sql`:
`feedback_releases(receipt PRIMARY KEY, install_hash, released_at)` and
`feedback_releases_install`. The manual platform deploy workflow applies that
idempotent migration remotely, then checks both names in sqlite_master before
deploying the worker. The ledger is not deleted by feedback or audit retention;
it holds identifiers and timestamps only. Historical automatic/converted statuses
are not inferred as releases. Existing clean-install trust backfill remains
one-time and does not populate the ledger. Manual grants can recognize established
contributors while new releases accumulate.

Studio and TUI follow-up belongs on studio: decode typed params alongside existing
sentinels, preserve them through the server projection, and localize hourly/daily,
IP, global and item-cap presentation. Clients must never infer block status or
describe a retry hint as guaranteed admission. Add transport, wire/frontend parity
and adjacent submit/reply/replay interaction tests there.

## Adoption schema

`migrate-feedback-adoptions.sql` is additive and idempotent: `feedback_adoptions`
(unique per install and item, unique per receipt) and `feedback_level_state`
(high-water count, revocation marker). The manual platform deploy applies and
verifies it before the Worker deploys. It also marks, once (`revocation_backfilled_v1`), installs without live trust that were rejected, taken down or last untrusted before the deploy. Rows are never inferred from history; the
ledger survives report and audit retention. To roll back, redeploy the previous
Worker; the tables can stay.

## Feedback admin page

Read and triage in-app feedback in a browser instead of the `feedback-admin` CLI.

- **Where:** `https://crash.reasonix.io/admin/feedback`. It is not served on the
  `*.workers.dev` host (the host gate answers 404), only on the custom domain.
- **Token:** the page asks for `FEEDBACK_ADMIN_TOKEN`, the same secret the
  converter and the CLI use (a Worker secret: `wrangler secret put
  FEEDBACK_ADMIN_TOKEN`). It is kept in the tab's `sessionStorage` only, sent in
  the `Authorization` header, never placed in a URL, and dropped when the tab
  closes or on "退出".
- **List:** newest first; filter by status, kind, app version, nickname
  (substring) and install (id, or 8+ hex characters of the install hash); 25 per
  page. Backed by `GET /v1/admin/feedback/list` (`status`, `category`,
  `version`, `nickname`, `install`, `limit`, `before`).
- **Detail:** full text, device info, contact (shown only here), thread, and
  screenshots. Unreleased screenshots are fetched through the admin-only
  attachment endpoint, so they are visible here before anything is public.
- **Actions:** release (optionally publishing screenshots), answer, ask,
  reply, reject, delete screenshots, block the install.

Safeguards: five wrong tokens from one address within 15 minutes lock that
address out of the whole admin API for the rest of the window, a correct token
included. The lock lifts by itself after the 15-minute window; to clear it sooner, delete the `af:%` rows from `feedback_quota` (`wrangler d1 execute reasonix-crash --remote --command "DELETE FROM feedback_quota WHERE bucket LIKE 'af:%'"`). User text is only ever written with `textContent`, the page ships a
strict CSP (`script-src 'self'`, no inline script or style), `noindex`, and
`no-store`. The page and its assets contain no feedback data.
