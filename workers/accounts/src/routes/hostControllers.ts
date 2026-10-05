import { Hono } from "hono";
import type { Context } from "hono";
import type { AppEnv } from "../env";
import { repos } from "../db";
import { toControllerView, toHostControllerView } from "../db/remoteControllers";
import type { ControllerState } from "../db/remoteControllers";
import { ApiError } from "../http/errors";
import { enforceRateLimit, recordRateEvent } from "../http/d1RateLimit";
import {
  parseBody,
  parseQuery,
  ControllerIdParam,
  ControllerResolveSchema,
  HostControllerListQuerySchema,
} from "../lib/validation";
import { REMOTE_RATE_RULES } from "../config";

// Resolving requires the desktop's own device credential, not an account
// session. Nothing reads the outcome yet, so this is bookkeeping, not a gate.
const hostControllers = new Hono<AppEnv>();

async function authenticateHost(c: Context<AppEnv>): Promise<{ userId: number; hostId: string }> {
  const deviceId = c.req.header("x-reasonix-device-id") ?? "";
  const credential = c.req.header("x-reasonix-device-credential") ?? "";
  if (!/^[0-9a-f]{64}$/.test(deviceId) || !/^[0-9a-f]{64}$/.test(credential)) {
    throw new ApiError(401, "invalid_device", "The device credential is invalid or revoked.");
  }
  const authenticated = await repos(c.env).remoteDevices.authenticate(deviceId, credential);
  if (!authenticated) throw new ApiError(401, "invalid_device", "The device credential is invalid or revoked.");
  await enforceRateLimit(c.env, REMOTE_RATE_RULES.hostControllers, deviceId);
  return { userId: authenticated.userId, hostId: deviceId };
}

const controllerNotFound = () => new ApiError(404, "controller_not_found", "That device is unavailable.");

function controllerId(c: Context<AppEnv>): string {
  const parsed = ControllerIdParam.safeParse(c.req.param("id"));
  if (!parsed.success) throw controllerNotFound();
  return parsed.data;
}

hostControllers.get("/", async (c) => {
  const { userId, hostId } = await authenticateHost(c);
  const { state } = parseQuery(c, HostControllerListQuerySchema);
  const states: ControllerState[] = state === "all" ? ["pending", "active", "revoked"] : [state];
  const controllers = repos(c.env).remoteControllers;
  const rows = await controllers.list(userId, hostId, states);
  const views = [];
  for (const row of rows) {
    const candidate = row.state === "pending" ? await controllers.replaceCandidate(row) : null;
    views.push(toHostControllerView(row, candidate?.id ?? null));
  }
  return c.json({ controllers: views });
});

hostControllers.post("/:id/resolve", async (c) => {
  const { userId, hostId } = await authenticateHost(c);
  const id = controllerId(c);
  const { action, replaceId } = await parseBody(c, ControllerResolveSchema);
  const outcome = await repos(c.env).remoteControllers.resolve({
    userId, hostDeviceId: hostId, id, action, ...(replaceId ? { replaceId } : {}),
  });
  switch (outcome.kind) {
    case "not_found":
      throw controllerNotFound();
    case "not_pending":
      throw new ApiError(409, "controller_not_pending", "This request is no longer waiting for approval.");
    case "expired":
      throw new ApiError(410, "pending_expired", "This request expired before it was answered.");
    case "replace_target_invalid":
      throw new ApiError(409, "replace_target_invalid", "The device to replace is not an active device of this computer.");
    case "cap_reached":
      throw new ApiError(409, "controller_cap_reached", "This computer already has the most devices it allows. Replace one instead.");
    case "rejected":
      await recordRateEvent(c.env, REMOTE_RATE_RULES.pendingRejects, outcome.controller.requester_session_hash);
      return c.json({ controller: toControllerView(outcome.controller) });
    case "activated":
      return c.json({
        controller: toControllerView(outcome.controller),
        ...(outcome.revoked ? { revoked: toControllerView(outcome.revoked) } : {}),
      });
  }
});

hostControllers.post("/:id/revoke", async (c) => {
  const { userId, hostId } = await authenticateHost(c);
  const id = controllerId(c);
  const outcome = await repos(c.env).remoteControllers.revoke(userId, id, hostId);
  if (outcome.kind === "not_found") throw controllerNotFound();
  return c.json({ controller: toControllerView(outcome.controller) });
});

export default hostControllers;
