import { Hono } from "hono";
import type { AppEnv } from "../env";
import { repos } from "../db";
import { toControllerView, clampUaClass } from "../db/remoteControllers";
import { currentUser, requireRemoteWebSession } from "../http/auth";
import { ApiError } from "../http/errors";
import { enforceRateLimit, rateLimitReached } from "../http/d1RateLimit";
import {
  parseBody,
  parseQuery,
  ControllerChallengeSchema,
  ControllerEnrollSchema,
  ControllerIdParam,
  ControllerListQuerySchema,
} from "../lib/validation";
import {
  controllerIdFor,
  enrollProofMessage,
  importControllerKey,
  thumbprintOf,
  verifyProof,
} from "../auth/controllerProof";
import { REMOTE_CHALLENGE_TTL_MS, REMOTE_RATE_RULES } from "../config";

// Mounted under /me, so requireAuth has already run. These routes record which
// browsers have proven possession of a key; nothing here issues a grant.
const remoteControllers = new Hono<AppEnv>();

const hostNotFound = () => new ApiError(404, "device_not_found", "That device is unavailable.");
const controllerNotFound = () => new ApiError(404, "controller_not_found", "That device is unavailable.");

remoteControllers.post("/challenge", async (c) => {
  const { user, session } = requireRemoteWebSession(c);
  await enforceRateLimit(c.env, REMOTE_RATE_RULES.challenge, session.id);
  const { host, purpose } = await parseBody(c, ControllerChallengeSchema);
  const { remoteDevices, remoteChallenges } = repos(c.env);
  if (!(await remoteDevices.activeForUser(user.id, host))) throw hostNotFound();
  const challenge = await remoteChallenges.issue(
    { userId: user.id, hostDeviceId: host, sessionHash: session.id, purpose },
    REMOTE_CHALLENGE_TTL_MS,
  );
  return c.json(challenge);
});

remoteControllers.post("/enroll", async (c) => {
  const { user, session } = requireRemoteWebSession(c);
  await enforceRateLimit(c.env, REMOTE_RATE_RULES.enroll, session.id);
  const input = await parseBody(c, ControllerEnrollSchema);
  const { remoteDevices, remoteChallenges, remoteControllers: controllers } = repos(c.env);
  if (!(await remoteDevices.activeForUser(user.id, input.host))) throw hostNotFound();

  const thumbprint = await thumbprintOf(input.publicKey);
  const id = await controllerIdFor(input.host, thumbprint);
  if (input.controllerId && input.controllerId !== id) {
    throw new ApiError(422, "controller_id_mismatch", "The enrollment id does not belong to this key.");
  }
  const key = await importControllerKey(input.publicKey);
  if (!key) throw new ApiError(422, "invalid_key", "The public key is not a valid P-256 key.");

  const consumed = await remoteChallenges.consume(input.nonce, {
    userId: user.id, hostDeviceId: input.host, sessionHash: session.id, purpose: "enroll",
  });
  if (consumed === "expired") throw new ApiError(403, "challenge_expired", "The challenge has expired.");
  if (consumed === "invalid") throw new ApiError(403, "challenge_invalid", "The challenge is not valid.");
  const message = enrollProofMessage({ nonce: input.nonce, userId: user.id, hostDeviceId: input.host, thumbprint });
  if (!(await verifyProof(key, message, input.signature))) {
    throw new ApiError(403, "invalid_proof", "The signature does not prove possession of the key.");
  }

  const claimed = input.claimsId && input.claimsId !== id
    ? await controllers.activeById(user.id, input.host, input.claimsId)
    : null;
  const outcome = await controllers.enroll({
    userId: user.id,
    hostDeviceId: input.host,
    id,
    thumbprint,
    publicKey: input.publicKey,
    uaClass: clampUaClass(input.uaClass),
    claimsId: claimed?.id ?? null,
    sessionHash: session.id,
    pendingLocked: await rateLimitReached(c.env, REMOTE_RATE_RULES.pendingRejects, session.id),
  });
  if (outcome.kind === "pending_limit") {
    throw new ApiError(429, "pending_limit", "Too many devices are waiting for approval on the computer.");
  }
  if (outcome.kind === "pending_locked") {
    throw new ApiError(429, "pending_locked", "New devices cannot ask for approval from this sign-in right now.");
  }
  const controller = toControllerView(outcome.controller);
  if (controller.state === "revoked") {
    throw new ApiError(403, "controller_revoked", "This device was revoked. Start again with a new key.", { controller });
  }
  if (controller.state === "pending") {
    throw new ApiError(403, "controller_unconfirmed", "Waiting for approval on your computer.", { controller });
  }
  return c.json({ controller });
});

remoteControllers.get("/", async (c) => {
  const user = currentUser(c);
  const { host } = parseQuery(c, ControllerListQuerySchema);
  const { remoteDevices, remoteControllers: controllers } = repos(c.env);
  if (!(await remoteDevices.activeForUser(user.id, host))) throw hostNotFound();
  const rows = await controllers.list(user.id, host, ["pending", "active", "revoked"]);
  return c.json({ controllers: rows.map(toControllerView) });
});

remoteControllers.post("/:id/revoke", async (c) => {
  const user = currentUser(c);
  const session = c.get("session");
  const parsed = ControllerIdParam.safeParse(c.req.param("id"));
  if (!parsed.success || !session) throw controllerNotFound();
  await enforceRateLimit(c.env, REMOTE_RATE_RULES.revoke, session.id);
  const outcome = await repos(c.env).remoteControllers.revoke(user.id, parsed.data);
  if (outcome.kind === "not_found") throw controllerNotFound();
  return c.json({ controller: toControllerView(outcome.controller) });
});

export default remoteControllers;
