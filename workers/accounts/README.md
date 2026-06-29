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
  db/     users.ts  sessions.ts  emailTokens.ts  index.ts (repos factory)
  email/  index.ts (Mailer + templates)  resend.ts  types.ts
  http/   errors.ts  cors.ts  auth.ts (session middleware)  ratelimit.ts
  lib/    validation.ts (zod)  handle.ts
  routes/ auth.ts  me.ts  users.ts  health.ts
```

Design notes:

- **Sessions store `sha256(pepper:token)`** — the raw token only ever lives in the
  cookie, so a DB read can't resurrect a live session. `sessions.kind` (`web`/`cli`)
  is reserved so the future desktop/CLI device-flow login reuses this table.
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
| GET    | `/me`                      | ✓    | the signed-in account                   |
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
