import type { Env } from "./env";
import { attachmentUrl } from "./feedback_attachments";
import { verifyInstall } from "./feedback_auth";
import { jsonResponse } from "./feedback_http";
import { installLevel, levelProfile } from "./feedback_level";
import type { FeedbackRow, StoredAttachment } from "./feedback_types";

export const MINE_LIMIT = 50;
const SNIPPET_CHARS = 80;
const REPLIES_SHOWN = 20;
const REPLIES_FETCH = MINE_LIMIT * REPLIES_SHOWN;

// The gate and its outcomes stay private: a held report reads as received and a
// rejected one as closed, with no reason.
export function publicStatus(row: FeedbackRow): string {
  if (row.status === "held") return "received";
  if (row.status === "rejected") return "closed";
  return row.status;
}

export interface ReplyRow {
  id: number;
  receipt: string;
  author: string;
  body: string;
  created_at: string;
}

export async function repliesFor(env: Env, receipts: string[], limit: number): Promise<Map<string, ReplyRow[]>> {
  const out = new Map<string, ReplyRow[]>();
  if (receipts.length === 0) return out;
  const marks = receipts.map(() => "?").join(",");
  const { results } = await env.DB.prepare(`SELECT id, receipt, author, body, created_at FROM feedback_replies WHERE receipt IN (${marks}) ORDER BY id DESC LIMIT ?`)
    .bind(...receipts, limit)
    .all<ReplyRow>();
  for (const r of results) out.set(r.receipt, [r, ...(out.get(r.receipt) ?? [])]);
  return out;
}

export async function replyCounts(env: Env, receipts: string[]): Promise<Map<string, number>> {
  const out = new Map<string, number>();
  if (receipts.length === 0) return out;
  const marks = receipts.map(() => "?").join(",");
  const { results } = await env.DB.prepare(`SELECT receipt, COUNT(*) AS n FROM feedback_replies WHERE receipt IN (${marks}) GROUP BY receipt`)
    .bind(...receipts)
    .all<{ receipt: string; n: number }>();
  for (const r of results) out.set(r.receipt, r.n);
  return out;
}

export async function mineItems(env: Env, results: FeedbackRow[]) {
  const receipts = results.map((r) => r.receipt);
  const replies = await repliesFor(env, receipts, REPLIES_FETCH);
  const counts = await replyCounts(env, receipts);
  return results.map((r) => {
    const thread = (replies.get(r.receipt) ?? []).slice(-REPLIES_SHOWN);
    return {
      receipt: r.receipt,
      category: r.category,
      titleSnippet: [...r.body].slice(0, SNIPPET_CHARS).join(""),
      status: publicStatus(r),
      needsInput: r.status === "needs_info",
      replyCount: counts.get(r.receipt) ?? 0,
      replies: thread.map((t) => ({ id: t.id, author: t.author === "user" ? "user" : "maintainer", body: t.body, createdAt: t.created_at })),
      issueNumber: r.issue_number,
      issueUrl: r.issue_url,
      resolvedVersion: r.resolved_version,
      duplicateOf: r.duplicate_of,
      createdAt: r.created_at,
      updatedAt: r.updated_at,
    };
  });
}

export async function handleMine(request: Request, env: Env): Promise<Response> {
  const who = await verifyInstall(request, env);
  if (who instanceof Response) return who;
  const now = new Date();
  const { results } = await env.DB.prepare("SELECT * FROM feedback WHERE install_hash = ? ORDER BY created_at DESC LIMIT ?")
    .bind(who.installHash, MINE_LIMIT)
    .all<FeedbackRow>();
  return jsonResponse({
    profile: levelProfile(await installLevel(env, who.installHash, now), now),
    items: await mineItems(env, results),
  });
}

// The converter's view: never the contact, and only images a maintainer released.
export function pendingItem(row: FeedbackRow, origin: string, released: ReadonlySet<string>) {
  const attachments = (JSON.parse(row.attachments_json) as StoredAttachment[]).filter((a) => released.has(a.key));
  return {
    receipt: row.receipt,
    status: "received" as const,
    category: row.category,
    body: row.body,
    displayName: row.display_name,
    env: JSON.parse(row.env_json) as Record<string, string>,
    attachments: attachments.map((a) => ({ name: a.name, contentType: a.contentType, url: attachmentUrl(origin, a.key), released: true as const })),
    createdAt: row.created_at,
  };
}

export async function releasedKeys(env: Env, receipts: string[]): Promise<Set<string>> {
  if (receipts.length === 0) return new Set();
  const marks = receipts.map(() => "?").join(",");
  const { results } = await env.DB.prepare(`SELECT key FROM feedback_public_images WHERE receipt IN (${marks})`).bind(...receipts).all<{ key: string }>();
  return new Set(results.map((r) => r.key));
}
