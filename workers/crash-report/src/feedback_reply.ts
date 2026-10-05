import type { Env } from "./env";
import { verifyInstall } from "./feedback_auth";
import { isBlocked } from "./feedback_blocks";
import { readCappedText } from "./feedback_body";
import { ipHash } from "./feedback_crypto";
import { jsonResponse, refuse } from "./feedback_http";
import { installLevel } from "./feedback_level";
import { admit, refund, replyRefusal } from "./feedback_quota";
import { ReplyBody } from "./feedback_schema";
import { MAX_REPLIES_PER_ITEM, MAX_REPLY_BYTES } from "./feedback_types";
import { announce, type OpsWaiter } from "./ops_emit";
import { scrubSensitiveText } from "./scrub";

const REPLYABLE = ["needs_info", "answered", "recorded", "in_progress"];
const REPLYABLE_SQL = `(${REPLYABLE.map((s) => `'${s}'`).join(",")})`;

export async function handleUserReply(request: Request, env: Env, receipt: string, ctx?: OpsWaiter): Promise<Response> {
  const who = await verifyInstall(request, env);
  if (who instanceof Response) return who;
  const secret = env.FEEDBACK_TOKEN_SECRET ?? "";
  const ip = request.headers.get("cf-connecting-ip") ?? "unknown";
  const now = new Date();
  const level = await installLevel(env, who.installHash, now);
  const ipKey = await ipHash(secret, ip);
  const blocked = await isBlocked(env, [`install:${who.installHash}`, `ip:${ipKey}`], now);
  const text = await readCappedText(request, MAX_REPLY_BYTES * 2 + 512);
  if (text === null) return refuse("feedback.too_large", "reply too large");
  let raw: unknown;
  try {
    raw = JSON.parse(text);
  } catch {
    return refuse("feedback.invalid", "body is not valid JSON");
  }
  const parsed = ReplyBody.safeParse(raw);
  if (!parsed.success) return refuse("feedback.invalid", "reply must be 1-4096 bytes of text");

  const policy = { kind: "reply" as const, trusted: level.trusted, hourly: level.limits.replies, receipt, replyable: REPLYABLE };
  const admission = await admit(env, { ipKey, installHash: who.installHash }, now, policy, blocked, ip);
  if (admission.kind === "refused") return admission.response;
  // One transaction decides ownership, status and the per-item cap, stores the
  // reply and moves needs_info back to held, so none of it can land half-done.
  const at = now.toISOString();
  let res: D1Result;
  try {
    [res] = await env.DB.batch([
      env.DB.prepare(
        `INSERT INTO feedback_replies (receipt, author, body, handled, created_at)
         SELECT receipt, 'user', ?, CASE status WHEN 'needs_info' THEN 1 WHEN 'answered' THEN 2 ELSE 0 END, ? FROM feedback
         WHERE receipt = ? AND install_hash = ? AND status IN ${REPLYABLE_SQL}
           AND (SELECT COUNT(*) FROM feedback_replies WHERE receipt = ? AND author = 'user') < ?`,
      ).bind(scrubSensitiveText(parsed.data.body), at, receipt, who.installHash, receipt, MAX_REPLIES_PER_ITEM),
      env.DB.prepare(
        `UPDATE feedback SET status = CASE WHEN status = 'needs_info' THEN 'held' ELSE status END, updated_at = ?
         WHERE receipt = ? AND install_hash = ? AND changes() > 0`,
      ).bind(at, receipt, who.installHash),
    ]);
  } catch (err) {
    await refund(env, admission.buckets);
    throw err;
  }
  if ((res.meta?.changes ?? 0) === 0) {
    await refund(env, admission.buckets);
    return (await replyRefusal(env, who.installHash, policy, now)) ?? refuse("feedback.not_replyable", "this report cannot take replies");
  }
  const replyId = res.meta?.last_row_id ?? 0;
  announce(ctx, env, { t: "replied", receipt });
  return jsonResponse({ replyId }, 201);
}
