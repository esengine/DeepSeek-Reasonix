import { Hono } from "hono";
import type { AppEnv } from "../env";
import { toAccountUser } from "../types";
import { repos } from "../db";
import { formatUserCode } from "../db/deviceGrants";
import { requireAuth, currentUser } from "../http/auth";
import { authRateLimit } from "../http/ratelimit";
import { ApiError } from "../http/errors";
import { parseBody, parseQuery, DevicePollSchema, DeviceApproveSchema, DeviceCodeQuerySchema } from "../lib/validation";
import { DEVICE_CODE_TTL_MS, DEVICE_POLL_INTERVAL_S } from "../config";

const device = new Hono<AppEnv>();

// CLI/desktop begins sign-in: get a device code to poll and a user code the
// human types on the web to approve. Public, throttled per IP.
device.post("/start", authRateLimit, async (c) => {
  const { deviceCode, userCode, expiresAt } = await repos(c.env).deviceGrants.start({
    userAgent: c.req.header("user-agent") ?? "",
    ttlMs: DEVICE_CODE_TTL_MS,
    kind: "cli",
  });
  const base = c.env.APP_ORIGIN;
  const display = formatUserCode(userCode);
  return c.json({
    deviceCode,
    userCode: display,
    verificationUri: `${base}/device`,
    verificationUriComplete: `${base}/device?code=${encodeURIComponent(display)}`,
    interval: DEVICE_POLL_INTERVAL_S,
    expiresIn: Math.floor(DEVICE_CODE_TTL_MS / 1000),
    expiresAt,
  });
});

// CLI/desktop polls with its device code. Not under the per-IP limiter — polling
// is frequent by design; the slow_down hint plus the short TTL bound abuse.
device.post("/poll", async (c) => {
  const { deviceCode } = await parseBody(c, DevicePollSchema);
  const { deviceGrants, sessions, users } = repos(c.env);

  const claimed = await deviceGrants.claim(deviceCode);
  if (claimed) {
    const row = await users.byId(claimed.userId);
    if (!row || row.status !== "active") throw new ApiError(403, "account_unavailable", "This account is not available.");
    const token = await sessions.create(claimed.userId, { kind: claimed.kind, userAgent: claimed.userAgent });
    return c.json({ status: "complete", sessionToken: token, user: toAccountUser(row) });
  }

  const status = await deviceGrants.pollStatus(deviceCode);
  switch (status.kind) {
    case "pending":
      return c.json({ status: status.slowDown ? "slow_down" : "authorization_pending", interval: DEVICE_POLL_INTERVAL_S });
    case "denied":
      throw new ApiError(403, "access_denied", "The sign-in request was denied.");
    case "expired":
      throw new ApiError(410, "expired_token", "This sign-in request has expired. Run login again.");
    case "not_found":
      throw new ApiError(400, "invalid_grant", "Unknown or already-used device code.");
  }
});

// The approval screen (signed-in web session) fetches what it's about to
// authorize before the user confirms.
device.get("/info", authRateLimit, requireAuth, async (c) => {
  const { userCode } = parseQuery(c, DeviceCodeQuerySchema);
  const grant = await repos(c.env).deviceGrants.info(userCode);
  if (!grant) throw new ApiError(404, "invalid_user_code", "That code is invalid or has expired.");
  return c.json({ grant: { ...grant, userCode: formatUserCode(grant.userCode) } });
});

device.post("/approve", authRateLimit, requireAuth, async (c) => {
  const user = currentUser(c);
  const { userCode } = await parseBody(c, DeviceApproveSchema);
  const ok = await repos(c.env).deviceGrants.approve(userCode, user.id);
  if (!ok) throw new ApiError(400, "invalid_user_code", "That code is invalid or has expired. Double-check it and try again.");
  return c.json({ ok: true });
});

device.post("/deny", authRateLimit, requireAuth, async (c) => {
  const { userCode } = await parseBody(c, DeviceApproveSchema);
  await repos(c.env).deviceGrants.deny(userCode);
  return c.json({ ok: true });
});

export default device;
