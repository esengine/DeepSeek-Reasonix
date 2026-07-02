# reasonix-accounts

The account service for `reasonix.io`: email/password sign-up, email verification,
sessions, password reset, and public profiles. A Cloudflare Worker (Hono) backed
by D1. Runs at `id.reasonix.io`, separate from the internal crash dashboard.

This is the backend API only — there are no HTML pages. The web frontend (and,
later, the desktop/CLI) call these JSON endpoints.

## Architecture

```
src/
  index.ts            entry — exports the Hono app as the fetch handler
  app.ts              middleware + route wiring
  env.ts  types.ts  config.ts
  auth/   crypto.ts (PBKDF2 + token hashing)  cookies.ts (session cookie)
  db/     users.ts  sessions.ts  emailTokens.ts  deviceGrants.ts  index.ts (repos factory)
  email/  index.ts (Mailer + templates)  resend.ts  types.ts
  http/   errors.ts  cors.ts  auth.ts (cookie + Bearer session)  ratelimit.ts
  lib/    validation.ts (zod)  handle.ts
  routes/ auth.ts  device.ts  me.ts  users.ts  health.ts
```

Design notes:

- **Sessions store `sha256(pepper:token)`** — the raw token only ever lives in the
  cookie (web) or the client's credential store (CLI/desktop), so a DB read can't
  resurrect a live session. Protected routes accept the session from the `rxid`
  cookie or an `Authorization: Bearer <token>` header, so the same table serves
  both surfaces (`sessions.kind` = `web` | `cli`).
- **Device sign-in (RFC 8628-style)** lets the CLI/desktop authenticate without a
  browser redirect: `/device/start` issues a `device_code` (polled) and a short
  `user_code` (the human approves it on `APP_ORIGIN/device` while signed in). Only
  the device code's peppered hash is stored; the `cli` session token is minted on
  the winning poll (an atomic `DELETE … RETURNING` claim), so it never lands in the
  DB. Polling isn't IP-limited — a `slow_down` hint plus the 10-minute TTL bound it.
- **`password_hash` is nullable** on `users` so OAuth-only identities can be added
  later without a rebuild.
- **Registration is enumeration-safe**: the response never reveals whether an email
  already exists; login/forgot return generic messages too.
- **PBKDF2-HMAC-SHA256, 100k iterations** — the work factor is embedded in each
  hash. 100k is Cloudflare Workers' hard cap for PBKDF2 (it rejects higher counts).

## Endpoints

| Method | Path                       | Auth | Notes                                   |
| ------ | -------------------------- | ---- | --------------------------------------- |
| POST   | `/auth/register`           | —    | `{ email, password, displayName? }`     |
| GET    | `/auth/verify?token=`      | —    | email link → 302 to `APP_ORIGIN/login`  |
| POST   | `/auth/login`              | —    | sets `rxid` cookie, returns `{ user }`  |
| POST   | `/auth/logout`             | —    | clears session + cookie                 |
| POST   | `/auth/forgot`             | —    | `{ email }` → reset link                |
| POST   | `/auth/reset`              | —    | `{ token, password }`                    |
| POST   | `/auth/resend-verification`| —    | `{ email }`                              |
| POST   | `/device/start`            | —    | CLI begins sign-in → `{ deviceCode, userCode, verificationUri, interval, expiresIn }` |
| POST   | `/device/poll`             | —    | `{ deviceCode }` → `authorization_pending` \| `slow_down` \| `{ sessionToken, user }` |
| GET    | `/device/info?userCode=`   | ✓    | approval screen: what a `user_code` will authorize |
| POST   | `/device/approve`          | ✓    | `{ userCode }` — bind the pending grant to the signed-in user |
| POST   | `/device/deny`             | ✓    | `{ userCode }` — reject the pending grant |
| GET    | `/me`                      | ✓    | the signed-in account (cookie or Bearer) |
| PATCH  | `/me`                      | ✓    | `{ displayName?, bio?, avatarUrl?, handle? }` |
| POST   | `/me/password`             | ✓    | `{ currentPassword, newPassword }`      |
| DELETE | `/me`                      | ✓    | soft-delete the account                 |
| GET    | `/u/:handle`               | —    | public profile                          |
| GET    | `/health`                  | —    | liveness                                |

Errors are `{ "error": { "code": "...", "message": "..." } }` with a matching HTTP status.

## Configuration

`wrangler.toml` `[vars]` (non-secret): `APP_ORIGIN`, `ALLOWED_ORIGINS`,
`COOKIE_DOMAIN`, `EMAIL_PROVIDER` (`stub` | `resend`), `MAIL_FROM`, `ADMIN_EMAILS`.

Secrets (`wrangler secret put NAME`): `SESSION_PEPPER` (any long random string),
`RESEND_API_KEY` (only when `EMAIL_PROVIDER=resend`).

When `EMAIL_PROVIDER` isn't `resend` (or no key is set) the worker logs email links
to the console — enough to exercise every flow locally without a mail provider.

## Local development

```sh
pnpm install
pnpm db:apply:local                 # create local D1 tables
pnpm dev                            # wrangler dev on http://localhost:8787
```

Put local secrets in `.dev.vars` (git-ignored), e.g. `SESSION_PEPPER="dev-pepper"`.
Register a user, then read the verification link from the `wrangler dev` console.

## Deploy

```sh
wrangler d1 create reasonix-accounts        # paste database_id into wrangler.toml
wrangler d1 migrations apply reasonix-accounts --remote
wrangler secret put SESSION_PEPPER
wrangler secret put RESEND_API_KEY          # if EMAIL_PROVIDER=resend
wrangler deploy
```

The `id.reasonix.io` custom domain route is declared in `wrangler.toml`; point the
DNS/custom-domain binding at this worker in the Cloudflare dashboard on first deploy.

The steps above are the one-time bootstrap. After that, every merge to `main-v2`
that touches `workers/accounts/**` redeploys via `.github/workflows/deploy-accounts-worker.yml`
(same pattern as the crash worker). CI does **not** run migrations — apply new ones
with `pnpm db:apply:remote` out of band.

`RESEND_API_KEY` is synced to the worker on each deploy from the `RESEND_API_KEY`
GitHub Actions repo secret (so the mail key has a single source of truth and needs
no local wrangler auth). `SESSION_PEPPER` is not in CI — set it once with
`wrangler secret put SESSION_PEPPER`.
