import type { Env } from "./env";
import { deleteAttachments, withdrawAttachments, ATTACHMENT_PREFIX } from "./feedback_attachments";
import { load, listLimit, readJson } from "./feedback_admin_store";
import { auditStatement, autoBlockStatement, grantTrustStatement, isBlocked, isTrusted, releaseLedgerStatement, revokeTrustStatement } from "./feedback_blocks";
import { jsonResponse, refuse } from "./feedback_http";
import { releasedKeys, repliesFor } from "./feedback_read";
import { RejectBody, ReleaseBody, ReplyBody } from "./feedback_schema";
import type { FeedbackRow, StoredAttachment } from "./feedback_types";
import { announce, type OpsWaiter } from "./ops_emit";
import { scrubSensitiveText } from "./scrub";

const TRIAGE = ["held", "needs_info"] as const;

// `status` guards the insert so a reply never lands on a report that moved elsewhere.
function replyStatements(env: Env, receipt: string, body: string, status?: string): D1PreparedStatement[] {
  const at = new Date().toISOString();
  return [
    env.DB.prepare(
      `INSERT INTO feedback_replies (receipt, author, body, handled, created_at)
       SELECT ?, 'maintainer', ?, 1, ? WHERE NOT EXISTS (SELECT 1 FROM feedback_replies WHERE receipt = ? AND author = 'maintainer' AND body = ?)
         AND (? IS NULL OR EXISTS (SELECT 1 FROM feedback WHERE receipt = ? AND status = ?))`,
    ).bind(receipt, body, at, receipt, body, status ?? null, receipt, status ?? null),
    env.DB.prepare("UPDATE feedback SET updated_at = ? WHERE receipt = ?").bind(at, receipt),
  ];
}

function attachmentsOf(row: FeedbackRow): StoredAttachment[] {
  return JSON.parse(row.attachments_json) as StoredAttachment[];
}

// Objects are deleted before the row forgets them, so a failure leaves a retryable trace.
async function dropAttachments(env: Env, row: FeedbackRow): Promise<void> {
  await withdrawAttachments(env, row.receipt, attachmentsOf(row));
  await env.DB.prepare("UPDATE feedback SET attachments_json = '[]' WHERE receipt = ?").bind(row.receipt).run();
}

export async function held(env: Env, url: URL): Promise<Response> {
  const limit = listLimit(url);
  const lists = await Promise.all(
    TRIAGE.map((s) => env.DB.prepare("SELECT * FROM feedback WHERE status = ? ORDER BY created_at ASC LIMIT ?").bind(s, limit).all<FeedbackRow>()),
  );
  const rows = lists.flatMap((l) => l.results).sort((a, b) => a.created_at.localeCompare(b.created_at)).slice(0, limit);
  const hashes = [...new Set(rows.map((r) => r.install_hash))];
  const trusted = new Set<string>();
  if (hashes.length > 0) {
    const marks = hashes.map(() => "?").join(",");
    const { results } = await env.DB.prepare(`SELECT install_hash FROM feedback_trust WHERE install_hash IN (${marks}) AND expires_at > ?`).bind(...hashes, new Date().toISOString()).all<{ install_hash: string }>();
    for (const r of results) trusted.add(r.install_hash);
  }
  const replies = await repliesFor(env, rows.map((r) => r.receipt), 500);
  return jsonResponse({
    items: rows.map((r) => ({
      receipt: r.receipt,
      status: r.status,
      category: r.category,
      body: r.body,
      displayName: r.display_name,
      contact: r.contact,
      env: JSON.parse(r.env_json) as Record<string, string>,
      attachments: attachmentsOf(r).map((a) => ({ key: a.key, name: a.name, contentType: a.contentType, size: a.size })),
      installHash: r.install_hash,
      installTrusted: trusted.has(r.install_hash),
      replies: (replies.get(r.receipt) ?? []).map((t) => ({ id: t.id, author: t.author, body: t.body, createdAt: t.created_at })),
      createdAt: r.created_at,
    })),
  });
}

export async function detail(env: Env, receipt: string): Promise<Response> {
  const row = await load(env, receipt);
  if (!row) return refuse("feedback.not_found", "unknown receipt");
  const released = await releasedKeys(env, [receipt]);
  const thread = (await repliesFor(env, [receipt], 500)).get(receipt) ?? [];
  return jsonResponse({
    receipt,
    status: row.status,
    category: row.category,
    body: row.body,
    displayName: row.display_name,
    contact: row.contact,
    env: JSON.parse(row.env_json) as Record<string, string>,
    attachments: attachmentsOf(row).map((a) => ({ key: a.key, name: a.name, contentType: a.contentType, size: a.size, released: released.has(a.key) })),
    installHash: row.install_hash,
    installTrusted: await isTrusted(env, row.install_hash),
    issueNumber: row.issue_number,
    issueUrl: row.issue_url,
    resolvedVersion: row.resolved_version,
    duplicateOf: row.duplicate_of,
    replies: thread.map((t) => ({ id: t.id, author: t.author, body: t.body, createdAt: t.created_at })),
    createdAt: row.created_at,
    updatedAt: row.updated_at,
  });
}

export async function release(request: Request, env: Env, receipt: string, ctx?: OpsWaiter): Promise<Response> {
  const body = ReleaseBody.safeParse((await readJson(request)) ?? {});
  if (!body.success) return refuse("feedback.invalid", "publishImages must be a boolean");
  const row = await load(env, receipt);
  if (!row) return refuse("feedback.not_found", "unknown receipt");
  if (row.status !== "held" && row.status !== "received") return refuse("feedback.bad_transition", "only held feedback can be released");
  const at = new Date().toISOString();
  // One transaction keeps release accounting, trust and image publication together.
  const steps = [
    env.DB.prepare("UPDATE feedback SET status = 'received', updated_at = ? WHERE receipt = ? AND status = 'held'").bind(at, receipt),
    releaseLedgerStatement(env, row.install_hash, receipt, at),
    grantTrustStatement(env, row.install_hash, receipt),
    ...(body.data.publishImages
      ? attachmentsOf(row).map((a) =>
          env.DB.prepare("INSERT OR IGNORE INTO feedback_public_images (key, receipt, created_at) SELECT ?, ?, ? WHERE EXISTS (SELECT 1 FROM feedback WHERE receipt = ? AND status = 'received')").bind(a.key, receipt, at, receipt),
        )
      : []),
  ];
  const res = await env.DB.batch(steps);
  if (row.status === "held" && (res[0].meta?.changes ?? 0) === 0) return refuse("feedback.bad_transition", "status changed concurrently");
  if (row.status === "held") announce(ctx, env, { t: "status", receipt, category: row.category, status: "received" });
  return jsonResponse({ receipt, status: "received", imagesPublished: body.data.publishImages });
}

export async function reject(request: Request, env: Env, receipt: string, ctx?: OpsWaiter): Promise<Response> {
  const body = RejectBody.safeParse(await readJson(request));
  if (!body.success) return refuse("feedback.invalid", "reason is required (1-200 characters)");
  const row = await load(env, receipt);
  if (!row) return refuse("feedback.not_found", "unknown receipt");
  if (row.status !== "rejected" && !(TRIAGE as readonly string[]).includes(row.status)) return refuse("feedback.bad_transition", "only held or needs_info feedback can be rejected");
  const at = new Date().toISOString();
  // State, trust, audit and auto-block commit together; repeating the call on an
  // already rejected item re-runs only the side effects that were missing.
  const res = await env.DB.batch([
    env.DB.prepare("UPDATE feedback SET status = 'rejected', contact = '', updated_at = ? WHERE receipt = ? AND status IN ('held','needs_info')").bind(at, receipt),
    env.DB.prepare("DELETE FROM feedback_trust WHERE install_hash = ? AND EXISTS (SELECT 1 FROM feedback WHERE receipt = ? AND status = 'rejected')").bind(row.install_hash, receipt),
    env.DB.prepare("INSERT INTO feedback_audit (at, action, detail) SELECT ?, 'reject', ? WHERE EXISTS (SELECT 1 FROM feedback WHERE receipt = ? AND status = 'rejected')")
      .bind(at, `${receipt} ${scrubSensitiveText(body.data.reason)}`.slice(0, 300), receipt),
    autoBlockStatement(env, row.install_hash),
  ]);
  if (row.status !== "rejected" && (res[0].meta?.changes ?? 0) === 0 && (await load(env, receipt))?.status !== "rejected") {
    return refuse("feedback.bad_transition", "status changed concurrently");
  }
  await dropAttachments(env, row);
  const blocked = await isBlocked(env, [`install:${row.install_hash}`], new Date());
  if (row.status !== "rejected") announce(ctx, env, { t: "status", receipt, category: row.category, status: "rejected" });
  return jsonResponse({ receipt, status: "rejected", installBlocked: blocked });
}

async function respond(request: Request, env: Env, receipt: string, to: "answered" | "needs_info", from: readonly string[], ctx?: OpsWaiter): Promise<Response> {
  const body = ReplyBody.safeParse(await readJson(request));
  if (!body.success) return refuse("feedback.invalid", "body must be 1-4096 bytes of text");
  const row = await load(env, receipt);
  if (!row) return refuse("feedback.not_found", "unknown receipt");
  if (row.status !== to && !from.includes(row.status)) return refuse("feedback.bad_transition", `feedback cannot move to ${to} from ${row.status}`);
  const clear = to === "answered" ? ", contact = ''" : "";
  const fromMarks = from.map(() => "?").join(",");
  // The reply commits with the state change; a repeat on the applied state only adds what is missing.
  const res = await env.DB.batch([
    env.DB.prepare(`UPDATE feedback SET status = '${to}'${clear}, updated_at = ? WHERE receipt = ? AND status IN (${fromMarks})`).bind(new Date().toISOString(), receipt, ...from),
    ...replyStatements(env, receipt, scrubSensitiveText(body.data.body), to),
  ]);
  if (row.status !== to && (res[0].meta?.changes ?? 0) === 0) return refuse("feedback.bad_transition", "status changed concurrently");
  if (row.status !== to) announce(ctx, env, { t: "status", receipt, category: row.category, status: to });
  return jsonResponse({ receipt, status: to });
}

export const answer = (request: Request, env: Env, receipt: string, ctx?: OpsWaiter) => respond(request, env, receipt, "answered", TRIAGE, ctx);
export const ask = (request: Request, env: Env, receipt: string, ctx?: OpsWaiter) => respond(request, env, receipt, "needs_info", ["held"], ctx);

export async function adminReply(request: Request, env: Env, receipt: string): Promise<Response> {
  const body = ReplyBody.safeParse(await readJson(request));
  if (!body.success) return refuse("feedback.invalid", "body must be 1-4096 bytes of text");
  const row = await load(env, receipt);
  if (!row) return refuse("feedback.not_found", "unknown receipt");
  if (row.status === "rejected") return refuse("feedback.not_replyable", "rejected feedback cannot take replies");
  await env.DB.batch(replyStatements(env, receipt, scrubSensitiveText(body.data.body)));
  return jsonResponse({ receipt, status: row.status });
}

export async function takedown(env: Env, receipt: string): Promise<Response> {
  const row = await load(env, receipt);
  if (!row) return refuse("feedback.not_found", "unknown receipt");
  const stored = attachmentsOf(row);
  if (env.TELEMETRY_RAW) await deleteAttachments(env.TELEMETRY_RAW, stored);
  await env.DB.batch([
    env.DB.prepare("UPDATE feedback SET attachments_json = '[]', updated_at = ? WHERE receipt = ?").bind(new Date().toISOString(), receipt),
    env.DB.prepare("DELETE FROM feedback_public_images WHERE receipt = ?").bind(receipt),
    revokeTrustStatement(env, row.install_hash),
    auditStatement(env, "takedown", `${receipt} removed=${stored.length}`),
  ]);
  return jsonResponse({ receipt, attachmentsRemoved: true, removed: stored.length });
}

// Triage needs to see an image before deciding to publish it; this is the only
// read path for an unreleased one and it sits behind the admin bearer.
export async function adminAttachment(env: Env, receipt: string, key: string): Promise<Response> {
  const row = await load(env, receipt);
  const a = row ? attachmentsOf(row).find((x) => x.key === key) : undefined;
  const obj = a && env.TELEMETRY_RAW ? await env.TELEMETRY_RAW.get(ATTACHMENT_PREFIX + key) : null;
  if (!a || !obj) return refuse("feedback.not_found", "unknown attachment");
  return new Response(obj.body, {
    headers: { "content-type": a.contentType, "x-content-type-options": "nosniff", "content-disposition": "attachment", "content-security-policy": "default-src 'none'; sandbox", "cache-control": "no-store" },
  });
}
