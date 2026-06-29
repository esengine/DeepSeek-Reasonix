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
import { parseBody, ProfileSchema, PasswordChangeSchema } from "../lib/validation";

const me = new Hono<AppEnv>();

// Everything under /me requires a session.
me.use("*", requireAuth);

me.get("/", (c) => c.json({ user: currentUser(c) }));

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
      if (await users.handleTaken(handle)) throw new ApiError(409, "handle_taken", "That handle is already taken.");
      update.handle = handle;
    }
  }
  if (patch.displayName !== undefined) update.displayName = patch.displayName;
  if (patch.bio !== undefined) update.bio = patch.bio;
  if (patch.avatarUrl !== undefined) update.avatarUrl = patch.avatarUrl;

  const row = await users.updateProfile(user.id, update);
  return c.json({ user: toAccountUser(row) });
});

me.post("/password", async (c) => {
  const user = currentUser(c);
  const { currentPassword, newPassword } = await parseBody(c, PasswordChangeSchema);
  const { users, sessions } = repos(c.env);

  const row = await users.byId(user.id);
  if (!row || !(await verifyPassword(currentPassword, row.password_hash))) {
    throw new ApiError(400, "invalid_password", "Your current password is incorrect.");
  }
  await users.updatePassword(user.id, await hashPassword(newPassword));

  // Drop every session, then mint a fresh one so this device stays signed in.
  await sessions.deleteAllForUser(user.id);
  setSessionCookie(c, await sessions.create(user.id, { userAgent: c.req.header("user-agent") ?? "" }));
  return c.json({ ok: true });
});

me.delete("/", async (c) => {
  const user = currentUser(c);
  const { users, sessions } = repos(c.env);
  await users.softDelete(user.id);
  await sessions.deleteAllForUser(user.id);
  clearSessionCookie(c);
  return c.json({ ok: true });
});

export default me;
