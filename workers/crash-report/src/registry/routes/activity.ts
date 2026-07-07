import { Hono } from "hono";
import { z } from "zod";
import type { AppEnv } from "../env";
import { repos } from "../db";
import { parseQuery } from "../lib/validation";

const activity = new Hono<AppEnv>();

const FeedQuerySchema = z.object({
  limit: z.coerce.number().int().min(1).max(100).default(30),
});

// The homepage live feed: publish/update/star/milestone events. Raw install
// pings are excluded here (they feed the trending rank) so the feed stays social.
activity.get("/", async (c) => {
  const { limit } = parseQuery(c, FeedQuerySchema);
  const rows = await repos(c.env).events.recent(limit);
  const events = rows.map((e) => ({
    type: e.type,
    slug: e.slug,
    actor: e.actor_handle,
    summary: e.summary,
    createdAt: e.created_at,
  }));
  return c.json({ events });
});

export default activity;
