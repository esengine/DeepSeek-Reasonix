import { Hono, type Context } from "hono";
import type { AppEnv } from "../env";
import { toListedPackageDTO, toPackageDTO } from "../types";
import { repos } from "../db";
import { requireAuth, currentUser } from "../http/auth";
import { DETAIL_CACHE_CONTROL, publicRead } from "../http/cache";
import { writeRateLimit } from "../http/ratelimit";
import type { OpsWaiter } from "../../ops_emit";
import { announceRegistryPending } from "../../ops_registry";
import { ApiError } from "../http/errors";
import { parseBody, parseQuery, PublishSchema, ListQuerySchema, VersionQuerySchema } from "../lib/validation";

const packages = new Hono<AppEnv>();

// executionCtx throws when the app is driven without one.
function waiter(c: Context<AppEnv>): OpsWaiter | undefined {
  try {
    return c.executionCtx;
  } catch {
    return undefined;
  }
}

const now = () => new Date().toISOString();

packages.get("/", publicRead(ListQuerySchema), async (c) => {
  const q = parseQuery(c, ListQuerySchema);
  const rows = await repos(c.env).packages.list({ ...q, now: now() });
  return c.json({ packages: rows.map(toListedPackageDTO), limit: q.limit, offset: q.offset });
});

packages.get("/:handle/:name", publicRead(VersionQuerySchema, DETAIL_CACHE_CONTROL), async (c) => {
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
  if (row.status === "pending") {
    announceRegistryPending(waiter(c), c.env, { slug: row.slug, version, kind: row.kind, source: row.source, summary: row.summary });
  }
  return c.json({ package: toPackageDTO(row), created, version }, created ? 201 : 200);
});

export default packages;
