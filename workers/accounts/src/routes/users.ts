import { Hono } from "hono";
import type { AppEnv } from "../env";
import { toPublicUser } from "../types";
import { repos } from "../db";
import { ApiError } from "../http/errors";

const users = new Hono<AppEnv>();

// Public profile. Suspended/deleted accounts are indistinguishable from missing.
users.get("/:handle", async (c) => {
  const handle = c.req.param("handle").toLowerCase();
  const row = await repos(c.env).users.byHandle(handle);
  if (!row || row.status !== "active") throw new ApiError(404, "not_found", "No such profile.");
  return c.json({ user: toPublicUser(row) });
});

export default users;
