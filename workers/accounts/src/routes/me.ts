import { Hono } from "hono";
import type { AppEnv } from "../env";
import { toAccountUser } from "../types";
import { repos } from "../db";
import type { ProfilePatch } from "../db/users";
import { requireAuth, currentUser } from "../http/auth";
import { ApiError } from "../http/errors";
import { hashPassword, verifyPassword } from "../auth/crypto";
import { setSessionCookie, clearSessionCookie } from "../auth/cookies";
import { isValidHandle } from "../lib/handle";
import {
  parseBody,
  ProfileSchema,
  PasswordChangeSchema,
  RemoteDeviceRegisterSchema,
  RemoteDeviceRenameSchema,
  RemoteGrantIssueSchema,
  RemoteAttachmentIssueSchema,
} from "../lib/validation";
import {
  REMOTE_ATTACHMENT_MAX_BYTES,
  REMOTE_ATTACHMENT_TTL_MS,
  REMOTE_GRANT_TTL_MS,
} from "../config";

const me = new Hono<AppEnv>();

// Everything under /me requires a session.
me.use("*", requireAuth);

me.get("/", (c) => c.json({ user: currentUser(c) }));

me.get("/devices", async (c) => {
  const user = currentUser(c);
  return c.json({ devices: await repos(c.env).remoteDevices.listForUser(user.id) });
});

me.post("/devices", async (c) => {
  const user = currentUser(c);
  const input = await parseBody(c, RemoteDeviceRegisterSchema);
  const registered = await repos(c.env).remoteDevices.register({ userId: user.id, ...input });
  return c.json(registered, 201);
});

me.patch("/devices/:deviceId", async (c) => {
  const user = currentUser(c);
  const { name } = await parseBody(c, RemoteDeviceRenameSchema);
  const device = await repos(c.env).remoteDevices.rename(user.id, c.req.param("deviceId"), name);
  if (!device) throw new ApiError(404, "device_not_found", "That device is unavailable.");
  return c.json({ device });
});

me.delete("/devices/:deviceId", async (c) => {
  const user = currentUser(c);
  const revoked = await repos(c.env).remoteDevices.revoke(user.id, c.req.param("deviceId"));
  if (!revoked) throw new ApiError(404, "device_not_found", "That device is unavailable.");
  return c.json({ ok: true });
});

me.post("/remote-grants", async (c) => {
  const user = currentUser(c);
  const { targetDeviceId, scopes } = await parseBody(c, RemoteGrantIssueSchema);
  const remoteDevices = repos(c.env).remoteDevices;
  const device = await remoteDevices.activeForUser(user.id, targetDeviceId);
  if (!device) throw new ApiError(404, "device_not_found", "That device is unavailable.");
  if (scopes.some((scope) => !device.capabilities.includes(scope))) {
    throw new ApiError(403, "scope_unavailable", "The device does not allow one or more requested capabilities.");
  }
  const grant = await remoteDevices.issueGrant({
    userId: user.id,
    targetDeviceId,
    scopes,
    ttlMs: REMOTE_GRANT_TTL_MS,
  });
  return c.json({ grant: { ...grant, targetDeviceId, scopes } }, 201);
});

me.post("/remote-attachments", async (c) => {
  const user = currentUser(c);
  const { targetDeviceId, ciphertextBytes } = await parseBody(c, RemoteAttachmentIssueSchema);
  const repositories = repos(c.env);
  const device = await repositories.remoteDevices.activeForUser(user.id, targetDeviceId);
  if (!device) throw new ApiError(404, "device_not_found", "That device is unavailable.");
  const attachment = await repositories.remoteAttachments.issue({
    userId: user.id,
    targetDeviceId,
    maxBytes: Math.min(ciphertextBytes, REMOTE_ATTACHMENT_MAX_BYTES),
    ttlMs: REMOTE_ATTACHMENT_TTL_MS,
  });
  return c.json({ attachment }, 201);
});

me.patch("/", async (c) => {
  const user = currentUser(c);
  const patch = await parseBody(c, ProfileSchema);
  const { users } = repos(c.env);

  const update: ProfilePatch = {};
  if (patch.handle !== undefined) {
    const handle = patch.handle.toLowerCase();
    if (!isValidHandle(handle)) {
      throw new ApiError(422, "invalid_handle", "Handles are 3–30 chars: letters, numbers, and underscores.");
    }
    if (handle !== user.handle) {
      update.handle = handle;
    }
  }
  if (patch.displayName !== undefined) update.displayName = patch.displayName;
  if (patch.bio !== undefined) update.bio = patch.bio;
  if (patch.avatarUrl !== undefined) update.avatarUrl = patch.avatarUrl;

  const row = await users.updateProfile(user.id, update);
  if (!row) throw new ApiError(409, "handle_taken", "That handle is already taken.");
  return c.json({ user: toAccountUser(row) });
});

me.post("/password", async (c) => {
  const user = currentUser(c);
  const { currentPassword, newPassword } = await parseBody(c, PasswordChangeSchema);
  const { users, sessions, remoteDevices } = repos(c.env);

  const row = await users.byId(user.id);
  if (!row || !(await verifyPassword(currentPassword, row.password_hash))) {
    throw new ApiError(400, "invalid_password", "Your current password is incorrect.");
  }
  await users.updatePassword(user.id, await hashPassword(newPassword));

  // Drop every session, then mint a fresh one so this device stays signed in.
  await sessions.deleteAllForUser(user.id);
  await remoteDevices.revokeAllForUser(user.id);
  setSessionCookie(c, await sessions.create(user.id, { userAgent: c.req.header("user-agent") ?? "" }));
  return c.json({ ok: true });
});

me.delete("/", async (c) => {
  const user = currentUser(c);
  const { users, sessions, remoteDevices } = repos(c.env);
  await users.softDelete(user.id);
  await sessions.deleteAllForUser(user.id);
  await remoteDevices.revokeAllForUser(user.id);
  clearSessionCookie(c);
  return c.json({ ok: true });
});

export default me;
