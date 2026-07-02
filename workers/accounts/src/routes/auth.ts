import { Hono } from "hono";
import type { AppEnv, Bindings } from "../env";
import type { Role } from "../types";
import { toAccountUser } from "../types";
import { repos, UserRepo } from "../db";
import { buildMailer, verifyEmail, resetEmail, accountExistsEmail } from "../email";
import { hashPassword, verifyPassword, randomSuffix } from "../auth/crypto";
import { setSessionCookie, clearSessionCookie, readSessionToken } from "../auth/cookies";
import { authRateLimit } from "../http/ratelimit";
import { readBearerToken } from "../http/auth";
import { ApiError } from "../http/errors";
import { deriveHandleBase, isValidHandle } from "../lib/handle";
import { parseBody, RegisterSchema, LoginSchema, ForgotSchema, ResetSchema, ResendSchema } from "../lib/validation";
import { VERIFY_TTL_MS, RESET_TTL_MS } from "../config";

const auth = new Hono<AppEnv>();

// Throttle every auth endpoint per source IP.
auth.use("*", authRateLimit);

function isAdminEmail(env: Bindings, email: string): boolean {
  const list = (env.ADMIN_EMAILS ?? "")
    .split(",")
    .map((s) => s.trim().toLowerCase())
    .filter(Boolean);
  return list.includes(email);
}

// Find a free, valid handle starting from a derived base, appending digits on
// collision and falling back to a random handle as a last resort.
async function uniqueHandle(users: UserRepo, base: string): Promise<string> {
  if (isValidHandle(base) && !(await users.handleTaken(base))) return base;
  for (let i = 0; i < 5; i++) {
    const candidate = `${base}${randomSuffix(i < 2 ? 2 : 4)}`.slice(0, 30).replace(/_+$/g, "");
    if (isValidHandle(candidate) && !(await users.handleTaken(candidate))) return candidate;
  }
  return `user${randomSuffix(8)}`;
}

function verifyLink(c: { req: { url: string } }, token: string): string {
  return `${new URL(c.req.url).origin}/auth/verify?token=${token}`;
}

// Register is deliberately enumeration-safe: the response is identical whether or
// not the email already exists. New emails get a verification link; existing ones
// get a resend or an "account already exists" nudge — never a different status.
auth.post("/register", async (c) => {
  const { email, password, displayName } = await parseBody(c, RegisterSchema);
  const { users, emailTokens } = repos(c.env);
  const mailer = buildMailer(c.env);
  const existing = await users.byEmail(email);

  if (!existing) {
    const handle = await uniqueHandle(users, deriveHandleBase(email));
    const role: Role = isAdminEmail(c.env, email) ? "admin" : "member";
    const user = await users.create({ handle, email, passwordHash: await hashPassword(password), displayName: displayName ?? "", role });
    const token = await emailTokens.issue(user.id, "verify", VERIFY_TTL_MS);
    await mailer.send({ to: email, ...verifyEmail(verifyLink(c, token)) });
  } else if (existing.email_verified === 0) {
    await emailTokens.invalidateForUser(existing.id, "verify");
    const token = await emailTokens.issue(existing.id, "verify", VERIFY_TTL_MS);
    await mailer.send({ to: email, ...verifyEmail(verifyLink(c, token)) });
  } else {
    await mailer.send({ to: email, ...accountExistsEmail(`${c.env.APP_ORIGIN}/login`, `${c.env.APP_ORIGIN}/forgot`) });
  }

  return c.json({ ok: true, message: "If that address is valid, check your inbox to confirm your account." });
});

// Email link target. Always lands the browser back on the site; never leaks
// whether the token was good beyond the ?verified flag.
auth.get("/verify", async (c) => {
  const token = c.req.query("token") ?? "";
  const { emailTokens, users } = repos(c.env);
  let ok = false;
  if (token.length >= 10) {
    const userId = await emailTokens.consume(token, "verify");
    if (userId !== null) {
      await users.markEmailVerified(userId);
      ok = true;
    }
  }
  return c.redirect(`${c.env.APP_ORIGIN}/login?verified=${ok ? "1" : "0"}`, 302);
});

auth.post("/login", async (c) => {
  const { email, password } = await parseBody(c, LoginSchema);
  const { users, sessions } = repos(c.env);
  const user = await users.byEmail(email);
  const ok = user ? await verifyPassword(password, user.password_hash) : false;
  if (!user || !ok) throw new ApiError(401, "invalid_credentials", "Incorrect email or password.");
  if (user.status !== "active") throw new ApiError(403, "account_unavailable", "This account is not available.");

  const token = await sessions.create(user.id, { userAgent: c.req.header("user-agent") ?? "" });
  setSessionCookie(c, token);
  return c.json({ user: toAccountUser(user) });
});

auth.post("/logout", async (c) => {
  const token = readSessionToken(c) ?? readBearerToken(c);
  if (token) await repos(c.env).sessions.deleteByToken(token);
  clearSessionCookie(c);
  return c.json({ ok: true });
});

auth.post("/forgot", async (c) => {
  const { email } = await parseBody(c, ForgotSchema);
  const { users, emailTokens } = repos(c.env);
  const user = await users.byEmail(email);
  if (user && user.status === "active") {
    await emailTokens.invalidateForUser(user.id, "reset");
    const token = await emailTokens.issue(user.id, "reset", RESET_TTL_MS);
    await buildMailer(c.env).send({ to: email, ...resetEmail(`${c.env.APP_ORIGIN}/reset?token=${token}`) });
  }
  return c.json({ ok: true, message: "If that account exists, a reset link is on its way." });
});

auth.post("/reset", async (c) => {
  const { token, password } = await parseBody(c, ResetSchema);
  const { users, emailTokens, sessions } = repos(c.env);
  const userId = await emailTokens.consume(token, "reset");
  if (userId === null) throw new ApiError(400, "invalid_token", "This reset link is invalid or has expired.");
  await users.updatePassword(userId, await hashPassword(password));
  await sessions.deleteAllForUser(userId); // force a fresh sign-in everywhere
  return c.json({ ok: true, message: "Password updated. You can now sign in." });
});

auth.post("/resend-verification", async (c) => {
  const { email } = await parseBody(c, ResendSchema);
  const { users, emailTokens } = repos(c.env);
  const user = await users.byEmail(email);
  if (user && user.email_verified === 0) {
    await emailTokens.invalidateForUser(user.id, "verify");
    const token = await emailTokens.issue(user.id, "verify", VERIFY_TTL_MS);
    await buildMailer(c.env).send({ to: email, ...verifyEmail(verifyLink(c, token)) });
  }
  return c.json({ ok: true, message: "If that address needs confirming, a new link is on its way." });
});

export default auth;
