import { Hono } from "hono";
import type { AppEnv } from "../env";
import { toPackageDTO } from "../types";
import { repos } from "../db";
import { requireAuth, currentUser } from "../http/auth";
import { writeRateLimit } from "../http/ratelimit";
import { ApiError } from "../http/errors";
import { parseBody, parseQuery, PublishSchema, ListQuerySchema, VersionQuerySchema } from "../lib/validation";

const packages = new Hono<AppEnv>();

const now = () => new Date().toISOString();

// Install-count thresholds worth announcing in the activity feed.
const MILESTONES = new Set([10, 50, 100, 500, 1000]);
const isMilestone = (n: number) => MILESTONES.has(n) || (n >= 1000 && n % 1000 === 0);

packages.get("/", async (c) => {
  const q = parseQuery(c, ListQuerySchema);
  const rows = await repos(c.env).packages.list({ ...q, now: now() });
  return c.json({ packages: rows.map(toPackageDTO), limit: q.limit, offset: q.offset });
});

packages.get("/:handle/:name", async (c) => {
  const slug = `${c.req.param("handle")}/${c.req.param("name")}`;
  const page = parseQuery(c, VersionQuerySchema);
  const { packages: repo } = repos(c.env);
  const row = await repo.bySlug(slug);
  if (!row || row.status !== "active") throw new ApiError(404, "not_found", "No such package.");
  const versions = await repo.versions(row.id, page);
  return c.json({ package: toPackageDTO(row), ...versions });
});

packages.post("/", writeRateLimit, requireAuth, async (c) => {
  const user = currentUser(c);
  if (!user.emailVerified) {
    throw new ApiError(403, "email_unverified", "Verify your email at id.reasonix.io before publishing.");
  }
  const input = await parseBody(c, PublishSchema);
  const { packages: repo, events } = repos(c.env);
  const { row, created, version } = await repo.publish(user, input, now());
  // Announce only what is public. A pending submission waits for an admin to
  // approve it before it surfaces in the feed or the listing.
  if (row.status === "active") {
    await events.log({
      type: created ? "publish" : "update",
      packageId: row.id,
      actorHandle: user.handle,
      summary: `${created ? "published" : "updated"} ${row.slug}@${version}`,
      now: now(),
    });
  }
  return c.json({ package: toPackageDTO(row), created, version }, created ? 201 : 200);
});

packages.post("/:handle/:name/installed", writeRateLimit, async (c) => {
  const slug = `${c.req.param("handle")}/${c.req.param("name")}`;
  const { packages: repo, events } = repos(c.env);
  const result = await repo.recordInstall(slug, now());
  if (result === null) throw new ApiError(404, "not_found", "No such package.");
  if (isMilestone(result.count)) {
    await events.log({
      type: "milestone",
      packageId: result.packageId,
      actorHandle: result.scopeHandle,
      summary: `${slug} reached ${result.count} installs`,
      now: now(),
    });
  }
  return c.json({ ok: true, installCount: result.count });
});

packages.post("/:handle/:name/star", writeRateLimit, requireAuth, async (c) => {
  const slug = `${c.req.param("handle")}/${c.req.param("name")}`;
  const user = currentUser(c);
  const { packages: repo, events } = repos(c.env);
  const result = await repo.toggleStar(slug, user.id, now());
  if (result === null) throw new ApiError(404, "not_found", "No such package.");
  if (result.starred) {
    await events.log({ type: "star", packageId: null, actorHandle: user.handle, summary: `starred ${slug}`, now: now() });
  }
  return c.json(result);
});

export default packages;
