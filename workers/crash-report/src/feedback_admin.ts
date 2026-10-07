import type { Env } from "./env";
import { ackReply, blockTarget, getCap, listBlocks, pendingReplies, recordAdoption, setCap, setReceiptTrust, tombstoneAdoption, triageReplies, unblockTarget } from "./feedback_admin_ops";
import { listLimit, load, readJson, setState, setStateStatement } from "./feedback_admin_store";
import { adoptionHolder, adoptionStatements, itemKeyOf, type AdoptionOutcome } from "./feedback_adoptions";
import { listFeedback } from "./feedback_admin_list";
import { requireAdmin } from "./feedback_auth";
import { jsonResponse, refuse } from "./feedback_http";
import { pendingItem, releasedKeys } from "./feedback_read";
import { announce, type OpsWaiter } from "./ops_emit";
import { LinkBody, RecordedBody, StatusBody } from "./feedback_schema";
import { adminAttachment, adminReply, answer, ask, detail, held, reject, release, takedown } from "./feedback_triage";
import { CONCRETE_VERSION, statusRank, type FeedbackRow } from "./feedback_types";

// Attachment URLs are public links, so they never follow the origin the admin request arrived on.
export const PUBLIC_ORIGIN = "https://crash.reasonix.io";
const OPEN_LIMIT = 200;
const ISSUE_BASE = "https://github.com/esengine/DeepSeek-Reasonix/issues/";
const LINKABLE = ["held", "needs_info", "answered", "received"];
const ACTIVE = ["recorded", "in_progress"];

async function pending(env: Env, url: URL): Promise<Response> {
  const { results } = await env.DB.prepare("SELECT * FROM feedback WHERE status = 'received' ORDER BY created_at ASC LIMIT ?").bind(listLimit(url)).all<FeedbackRow>();
  const released = await releasedKeys(env, results.map((r) => r.receipt));
  return jsonResponse({ items: results.map((r) => pendingItem(r, PUBLIC_ORIGIN, released)) });
}

async function open(env: Env): Promise<Response> {
  type Open = Pick<FeedbackRow, "receipt" | "status" | "issue_number" | "issue_url">;
  const cols = "SELECT receipt, status, issue_number, issue_url FROM feedback";
  const active = await env.DB.prepare(`${cols} WHERE status IN ('recorded','in_progress') ORDER BY created_at ASC LIMIT ?`).bind(OPEN_LIMIT).all<Open>();
  const awaitingTag = await env.DB.prepare(`${cols} WHERE status = 'fixed' AND resolved_version = 'next' ORDER BY created_at ASC LIMIT ?`)
    .bind(OPEN_LIMIT)
    .all<Open>();
  const results = [...active.results, ...awaitingTag.results].slice(0, OPEN_LIMIT);
  return jsonResponse({
    items: results.map((r) => ({ receipt: r.receipt, status: r.status, issueNumber: r.issue_number, issueUrl: r.issue_url })),
  });
}

async function recorded(request: Request, env: Env, receipt: string, ctx?: OpsWaiter): Promise<Response> {
  const body = RecordedBody.safeParse(await readJson(request));
  if (!body.success) return refuse("feedback.invalid", "issueNumber and issueUrl are required");
  const row = await load(env, receipt);
  if (!row) return refuse("feedback.not_found", "unknown receipt");
  if (statusRank(row.status) >= 1) return jsonResponse({ receipt, status: row.status, issueNumber: row.issue_number, issueUrl: row.issue_url });
  if (row.status !== "received") return refuse("feedback.bad_transition", "feedback is not releasable to the converter");
  const ok = await setState(env, receipt, "received", "status = 'recorded', issue_number = ?, issue_url = ?", [body.data.issueNumber, body.data.issueUrl]);
  if (!ok) return refuse("feedback.bad_transition", "status changed concurrently");
  announce(ctx, env, { t: "status", receipt, category: row.category, status: "recorded" });
  return jsonResponse({ receipt, status: "recorded", issueNumber: body.data.issueNumber, issueUrl: body.data.issueUrl });
}

// A maintainer attaches an issue filed by hand to a report the converter never saw.
async function link(request: Request, env: Env, receipt: string, ctx?: OpsWaiter): Promise<Response> {
  const body = LinkBody.safeParse(await readJson(request));
  if (!body.success) return refuse("feedback.invalid", "issueNumber is required");
  const { issueNumber } = body.data;
  const issueUrl = `${ISSUE_BASE}${issueNumber}`;
  const row = await load(env, receipt);
  if (!row) return refuse("feedback.not_found", "unknown receipt");
  if (statusRank(row.status) >= 1) {
    if (row.issue_number !== issueNumber) return refuse("feedback.issue_conflict", "feedback is already linked to a different issue");
    return jsonResponse({ receipt, status: row.status, issueNumber: row.issue_number, issueUrl: row.issue_url });
  }
  if (!LINKABLE.includes(row.status)) return refuse("feedback.bad_transition", "only held, needs_info, answered or received feedback can be linked");
  const ok = await setState(env, receipt, row.status, "status = 'recorded', issue_number = ?, issue_url = ?", [issueNumber, issueUrl]);
  if (!ok) return refuse("feedback.bad_transition", "status changed concurrently");
  announce(ctx, env, { t: "status", receipt, category: row.category, status: "recorded" });
  return jsonResponse({ receipt, status: "recorded", issueNumber, issueUrl });
}

type Shipped = { applied: false } | { applied: true; adoption: AdoptionOutcome };

// A concrete published version is the only event that ships an outcome, so the
// credit commits with the status change or not at all.
async function shipFix(env: Env, row: FeedbackRow, sets: string, binds: unknown[], version: string): Promise<Shipped> {
  const state = setStateStatement(env, row.receipt, row.status, sets, binds);
  const key = itemKeyOf(row);
  if (!key) return (await state.run()).meta?.changes ? { applied: true, adoption: "no_item_key" } : { applied: false };
  const res = await env.DB.batch([state, ...adoptionStatements(env, row, key, version, "shipped", new Date().toISOString())]);
  if (!res[0].meta?.changes) return { applied: false };
  return { applied: true, adoption: res[1].meta?.changes ? "credited" : await adoptionHolder(env, row.receipt, key) };
}

async function status(request: Request, env: Env, receipt: string, ctx?: OpsWaiter): Promise<Response> {
  const body = StatusBody.safeParse(await readJson(request));
  if (!body.success) return refuse("feedback.invalid", "unknown or malformed status update");
  const { status: next, resolvedVersion, duplicateOf } = body.data;
  if (next === "fixed" && !resolvedVersion) return refuse("feedback.invalid", "fixed requires resolvedVersion or \"next\"");
  const row = await load(env, receipt);
  if (!row) return refuse("feedback.not_found", "unknown receipt");
  if (row.status === "fixed" && next === "fixed") {
    if (row.resolved_version === resolvedVersion) return jsonResponse({ receipt, status: "fixed" });
    if (row.resolved_version !== "next" || !resolvedVersion || !CONCRETE_VERSION.test(resolvedVersion)) {
      return refuse("feedback.bad_transition", "a fixed report can only move from \"next\" to a concrete version");
    }
    const shipped = await shipFix(env, row, "resolved_version = ?", [resolvedVersion], resolvedVersion);
    if (!shipped.applied) return refuse("feedback.bad_transition", "status changed concurrently");
    return jsonResponse({ receipt, status: "fixed", adoption: shipped.adoption });
  }
  if (row.status === next) return jsonResponse({ receipt, status: row.status });
  if (!ACTIVE.includes(row.status) || statusRank(next) <= statusRank(row.status)) {
    return refuse("feedback.bad_transition", "only forward transitions from a recorded report are allowed");
  }
  const sets = `status = ?, resolved_version = ?, duplicate_of = ?${next === "in_progress" ? "" : ", contact = ''"}`;
  const binds = [next, next === "fixed" ? (resolvedVersion ?? null) : null, next === "duplicate" ? (duplicateOf ?? null) : null];
  if (next === "fixed" && resolvedVersion && CONCRETE_VERSION.test(resolvedVersion)) {
    const shipped = await shipFix(env, row, sets, binds, resolvedVersion);
    if (!shipped.applied) return refuse("feedback.bad_transition", "status changed concurrently");
    announce(ctx, env, { t: "status", receipt, category: row.category, status: next });
    return jsonResponse({ receipt, status: next, adoption: shipped.adoption });
  }
  if (!(await setState(env, receipt, row.status, sets, binds))) return refuse("feedback.bad_transition", "status changed concurrently");
  announce(ctx, env, { t: "status", receipt, category: row.category, status: next });
  return jsonResponse({ receipt, status: next });
}

const NOT_ALLOWED = () => refuse("feedback.method_not_allowed", "method not allowed");

export async function handleAdmin(request: Request, env: Env, url: URL, ctx?: OpsWaiter): Promise<Response | null> {
  const path = url.pathname;
  const m = path.match(/^\/v1\/admin\/feedback\/(?:(pending|open|held|list|blocks?|cap)|(replies)\/(pending|triage)|replies\/([A-Za-z0-9_-]{1,64})\/ack|(FB-[0-9A-Z]{4}-[0-9A-Z]{4})(?:\/(recorded|link|status|release|reject|answer|ask|reply|takedown|trust|adoption)|\/attachments\/([A-Za-z0-9_-]{16,64}))?)$/);
  if (!m) return null;
  const denied = await requireAdmin(request, env);
  if (denied) return denied;
  const method = request.method;
  const [, name, , , ackId, receipt, action, attachmentKey] = m;
  if (m[2] === "replies") return method !== "GET" ? NOT_ALLOWED() : m[3] === "triage" ? triageReplies(env, url) : pendingReplies(env, url);
  if (ackId) return method === "POST" ? ackReply(env, ackId) : NOT_ALLOWED();
  if (name) {
    if (name === "block") {
      if (method === "POST") return blockTarget(request, env);
      return method === "DELETE" ? unblockTarget(request, env) : NOT_ALLOWED();
    }
    if (name === "cap") {
      if (method === "POST") return setCap(request, env);
      return method === "GET" ? getCap(env) : NOT_ALLOWED();
    }
    if (method !== "GET") return NOT_ALLOWED();
    if (name === "pending") return pending(env, url);
    if (name === "open") return open(env);
    if (name === "held") return held(env, url);
    if (name === "list") return listFeedback(env, url);
    return listBlocks(env);
  }
  if (attachmentKey) return method === "GET" ? adminAttachment(env, receipt, attachmentKey) : NOT_ALLOWED();
  if (!action) return method === "GET" ? detail(env, receipt) : NOT_ALLOWED();
  if (action === "adoption") return method === "POST" ? recordAdoption(request, env, receipt) : method === "DELETE" ? tombstoneAdoption(env, receipt) : NOT_ALLOWED();
  if (action === "trust") return method === "POST" || method === "DELETE" ? setReceiptTrust(env, receipt, method === "POST") : NOT_ALLOWED();
  if (method !== "POST") return NOT_ALLOWED();
  switch (action) {
    case "recorded":
      return recorded(request, env, receipt, ctx);
    case "link":
      return link(request, env, receipt, ctx);
    case "status":
      return status(request, env, receipt, ctx);
    case "release":
      return release(request, env, receipt, ctx);
    case "reject":
      return reject(request, env, receipt, ctx);
    case "answer":
      return answer(request, env, receipt, ctx);
    case "ask":
      return ask(request, env, receipt, ctx);
    case "reply":
      return adminReply(request, env, receipt, ctx);
    default:
      return takedown(env, receipt);
  }
}
