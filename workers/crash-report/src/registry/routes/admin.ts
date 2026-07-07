import { Hono } from "hono";
import type { AppEnv } from "../env";
import { toPackageDTO } from "../types";
import { repos } from "../db";
import { requireAdmin } from "../http/auth";
import { writeRateLimit } from "../http/ratelimit";
import { ApiError } from "../http/errors";

const admin = new Hono<AppEnv>();

const now = () => new Date().toISOString();

admin.use("*", requireAdmin);

// Review queue. ?status defaults to pending; also accepts rejected/hidden/active.
admin.get("/packages", async (c) => {
  const status = new URL(c.req.url).searchParams.get("status") || "pending";
  const rows = await repos(c.env).packages.listByStatus(status, 200);
  return c.json({ packages: rows.map(toPackageDTO) });
});

// Approve a pending package → live. Its publish event is emitted here (on first
// approval), so the activity feed only ever announces public packages.
admin.post("/packages/:handle/:name/approve", writeRateLimit, async (c) => {
  const slug = `${c.req.param("handle")}/${c.req.param("name")}`;
  const { packages: repo, events } = repos(c.env);
  const before = await repo.bySlug(slug);
  if (!before) throw new ApiError(404, "not_found", "No such package.");
  const row = await repo.setStatus(slug, "active", now());
  if (!row) throw new ApiError(404, "not_found", "No such package.");
  if (before.status !== "active") {
    await events.log({
      type: "publish",
      packageId: row.id,
      actorHandle: row.scope_handle,
      summary: `published ${row.slug}@${row.latest_version}`,
      now: now(),
    });
  }
  return c.json({ package: toPackageDTO(row) });
});

admin.post("/packages/:handle/:name/reject", writeRateLimit, async (c) => {
  const slug = `${c.req.param("handle")}/${c.req.param("name")}`;
  const row = await repos(c.env).packages.setStatus(slug, "rejected", now());
  if (!row) throw new ApiError(404, "not_found", "No such package.");
  return c.json({ package: toPackageDTO(row) });
});

// Take a previously-approved package back down.
admin.post("/packages/:handle/:name/hide", writeRateLimit, async (c) => {
  const slug = `${c.req.param("handle")}/${c.req.param("name")}`;
  const row = await repos(c.env).packages.setStatus(slug, "hidden", now());
  if (!row) throw new ApiError(404, "not_found", "No such package.");
  return c.json({ package: toPackageDTO(row) });
});

// Grant or revoke the verified badge. Body {verified:false} revokes; default grants.
admin.post("/packages/:handle/:name/verify", writeRateLimit, async (c) => {
  const slug = `${c.req.param("handle")}/${c.req.param("name")}`;
  const body = (await c.req.json().catch(() => ({}))) as { verified?: boolean };
  const row = await repos(c.env).packages.setVerified(slug, body.verified !== false, now());
  if (!row) throw new ApiError(404, "not_found", "No such package.");
  return c.json({ package: toPackageDTO(row) });
});

export default admin;
