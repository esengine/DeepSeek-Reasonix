// Reasonix Community forum API. Identity from id.reasonix.io; content + anti-abuse
// state in D1. The Hono app is itself the Workers fetch handler.
import { Hono } from "hono";
import { cors } from "hono/cors";
import { z } from "zod";
import type { AppEnv } from "./env";
import { loadMember, currentMember, HttpError } from "./identity";
import { assertCanPost, dailyPostCap, rateLimited, AUTO_HIDE_FLAGS } from "./antispam";

const app = new Hono<AppEnv>();

app.onError((err, c) => {
  if (err instanceof HttpError) return c.json({ error: { code: err.code, message: err.message } }, err.status as 400);
  console.error("forum error:", err);
  return c.json({ error: { code: "internal", message: "Something went wrong." } }, 500);
});

app.use("*", (c, next) => {
  const allowed = (c.env.ALLOWED_ORIGINS ?? "").split(",").map((s) => s.trim()).filter(Boolean);
  return cors({
    origin: (o) => (allowed.includes(o) ? o : null),
    credentials: true,
    allowMethods: ["GET", "POST", "PATCH", "DELETE", "OPTIONS"],
    allowHeaders: ["Content-Type", "Authorization"],
  })(c, next);
});
app.use("*", loadMember);

const slugify = (s: string) =>
  s.toLowerCase().replace(/[^a-z0-9一-鿿]+/g, "-").replace(/^-+|-+$/g, "").slice(0, 60) || "topic";

async function postsToday(c: { env: AppEnv["Bindings"] }, email: string): Promise<number> {
  const since = new Date(Date.now() - 86_400_000).toISOString();
  const row = await c.env.DB.prepare("SELECT COUNT(*) AS n FROM posts WHERE author = ?1 AND created_at > ?2")
    .bind(email, since)
    .first<{ n: number }>();
  return row?.n ?? 0;
}

async function enforceRate(c: { env: AppEnv["Bindings"]; req: { header: (k: string) => string | undefined } }, member: Parameters<typeof rateLimited>[0]): Promise<void> {
  if (!rateLimited(member)) return;
  const limiter = c.env.POST_LIMITER;
  if (limiter) {
    const ip = c.req.header("cf-connecting-ip") ?? member.email;
    const { success } = await limiter.limit({ key: ip });
    if (!success) throw new HttpError(429, "rate_limited", "You're posting too fast — take a short break.");
  }
  if ((await postsToday(c, member.email)) >= dailyPostCap(member.trust)) {
    throw new HttpError(429, "daily_limit", "You've hit today's posting limit for your trust level.");
  }
}

app.get("/health", (c) => c.json({ ok: true, service: "forum" }));

app.get("/categories", async (c) => {
  const rows = await c.env.DB.prepare(
    `SELECT c.id, c.slug, c.name, c.description, c.min_trust_to_post AS minTrust,
            (SELECT COUNT(*) FROM topics t WHERE t.category_id = c.id) AS topicCount,
            (SELECT MAX(last_post_at) FROM topics t WHERE t.category_id = c.id) AS lastActivity
     FROM categories c ORDER BY c.position, c.id`,
  ).all();
  return c.json({ categories: rows.results });
});

const TopicList = z.object({ category: z.string().optional(), sort: z.enum(["latest", "top"]).optional() });
app.get("/topics", async (c) => {
  const q = TopicList.parse(Object.fromEntries(new URL(c.req.url).searchParams));
  const order = q.sort === "top" ? "t.reply_count DESC, t.last_post_at DESC" : "t.pinned DESC, t.last_post_at DESC";
  const where = q.category ? "WHERE cat.slug = ?1 AND t.status != 'hidden'" : "WHERE t.status != 'hidden'";
  const stmt = c.env.DB.prepare(
    `SELECT t.id, t.title, t.slug, t.status, t.pinned, t.reply_count AS replyCount, t.view_count AS viewCount,
            t.author, t.created_at AS createdAt, t.last_post_at AS lastPostAt, cat.slug AS category, cat.name AS categoryName
     FROM topics t JOIN categories cat ON cat.id = t.category_id ${where} ORDER BY ${order} LIMIT 50`,
  );
  const rows = await (q.category ? stmt.bind(q.category) : stmt).all();
  return c.json({ topics: rows.results });
});

app.get("/topics/:id", async (c) => {
  const id = Number(c.req.param("id"));
  const topic = await c.env.DB.prepare(
    `SELECT t.id, t.title, t.slug, t.status, t.pinned, t.author, t.accepted_post_id AS acceptedPostId,
            t.reply_count AS replyCount, t.view_count AS viewCount, t.created_at AS createdAt, cat.slug AS category
     FROM topics t JOIN categories cat ON cat.id = t.category_id WHERE t.id = ?1 AND t.status != 'hidden'`,
  ).bind(id).first();
  if (!topic) throw new HttpError(404, "not_found", "That topic doesn't exist.");
  await c.env.DB.prepare("UPDATE topics SET view_count = view_count + 1 WHERE id = ?1").bind(id).run();
  const posts = await c.env.DB.prepare(
    `SELECT p.id, p.author, p.body, p.status, p.like_count AS likeCount, p.created_at AS createdAt, p.edited_at AS editedAt,
            m.handle, m.trust, m.role
     FROM posts p LEFT JOIN members m ON m.email = p.author
     WHERE p.topic_id = ?1 AND p.status IN ('visible') ORDER BY p.created_at`,
  ).bind(id).all();
  return c.json({ topic, posts: posts.results });
});

const NewTopic = z.object({
  categoryId: z.number().int().positive(),
  title: z.string().trim().min(6).max(160),
  body: z.string().trim().min(10).max(20000),
});
app.post("/topics", async (c) => {
  const member = currentMember(c);
  const input = NewTopic.parse(await c.req.json());
  const cat = await c.env.DB.prepare("SELECT id, min_trust_to_post AS minTrust FROM categories WHERE id = ?1")
    .bind(input.categoryId)
    .first<{ id: number; minTrust: number }>();
  if (!cat) throw new HttpError(404, "no_category", "That category doesn't exist.");
  assertCanPost(member, { minTrust: cat.minTrust, body: input.body });
  await enforceRate(c, member);

  const now = new Date().toISOString();
  const topicRes = await c.env.DB.prepare(
    `INSERT INTO topics (category_id, author, title, slug, created_at, last_post_at) VALUES (?1, ?2, ?3, ?4, ?5, ?5)`,
  )
    .bind(cat.id, member.email, input.title, slugify(input.title), now)
    .run();
  const topicId = Number(topicRes.meta.last_row_id);
  await c.env.DB.prepare("INSERT INTO posts (topic_id, author, body, created_at) VALUES (?1, ?2, ?3, ?4)")
    .bind(topicId, member.email, input.body, now)
    .run();
  await c.env.DB.prepare("UPDATE members SET post_count = post_count + 1 WHERE email = ?1").bind(member.email).run();
  return c.json({ topic: { id: topicId, slug: slugify(input.title) } }, 201);
});

const Reply = z.object({ body: z.string().trim().min(2).max(20000) });
app.post("/topics/:id/posts", async (c) => {
  const member = currentMember(c);
  const topicId = Number(c.req.param("id"));
  const input = Reply.parse(await c.req.json());
  const topic = await c.env.DB.prepare(
    "SELECT t.id, t.status, c.min_trust_to_post AS minTrust FROM topics t JOIN categories c ON c.id = t.category_id WHERE t.id = ?1",
  )
    .bind(topicId)
    .first<{ id: number; status: string; minTrust: number }>();
  if (!topic || topic.status === "hidden") throw new HttpError(404, "not_found", "That topic doesn't exist.");
  if (topic.status === "closed") throw new HttpError(403, "closed", "This topic is closed to new replies.");
  assertCanPost(member, { minTrust: topic.minTrust, body: input.body });
  await enforceRate(c, member);

  const now = new Date().toISOString();
  const res = await c.env.DB.prepare("INSERT INTO posts (topic_id, author, body, created_at) VALUES (?1, ?2, ?3, ?4)")
    .bind(topicId, member.email, input.body, now)
    .run();
  await c.env.DB.prepare("UPDATE topics SET reply_count = reply_count + 1, last_post_at = ?2 WHERE id = ?1")
    .bind(topicId, now)
    .run();
  await c.env.DB.prepare("UPDATE members SET post_count = post_count + 1 WHERE email = ?1").bind(member.email).run();
  return c.json({ post: { id: Number(res.meta.last_row_id) } }, 201);
});

const Flag = z.object({ reason: z.enum(["spam", "offensive", "off_topic", "other"]), note: z.string().trim().max(500).optional() });
app.post("/posts/:id/flags", async (c) => {
  const member = currentMember(c);
  const postId = Number(c.req.param("id"));
  const input = Flag.parse(await c.req.json());
  const post = await c.env.DB.prepare("SELECT id, status FROM posts WHERE id = ?1").bind(postId).first<{ id: number; status: string }>();
  if (!post) throw new HttpError(404, "not_found", "That post doesn't exist.");

  const now = new Date().toISOString();
  await c.env.DB.prepare(
    "INSERT INTO flags (post_id, reporter, reason, note, created_at) VALUES (?1, ?2, ?3, ?4, ?5) ON CONFLICT(post_id, reporter) DO NOTHING",
  )
    .bind(postId, member.email, input.reason, input.note ?? "", now)
    .run();
  const count = await c.env.DB.prepare("SELECT COUNT(*) AS n FROM flags WHERE post_id = ?1").bind(postId).first<{ n: number }>();
  const flagCount = count?.n ?? 0;
  await c.env.DB.prepare("UPDATE posts SET flag_count = ?2 WHERE id = ?1").bind(postId, flagCount).run();
  if (flagCount >= AUTO_HIDE_FLAGS && post.status === "visible") {
    await c.env.DB.prepare("UPDATE posts SET status = 'hidden' WHERE id = ?1").bind(postId).run();
    await c.env.DB.prepare("INSERT INTO mod_log (at, actor, action, target, detail) VALUES (?1, 'system', 'auto_hide_post', ?2, ?3)")
      .bind(now, String(postId), `${flagCount} flags`)
      .run();
  }
  return c.json({ ok: true, flagCount, hidden: flagCount >= AUTO_HIDE_FLAGS });
});

export default app;
