import type { Env } from "./env";
import { sendAlert } from "./alert";
import { decodeAttachment, deleteAttachments, storeAttachment } from "./feedback_attachments";
import { feedbackEnabled, isKnownInstall, tokenMatches } from "./feedback_auth";
import { readCappedText } from "./feedback_body";
import { installHash, installToken, ipHash, newReceipt } from "./feedback_crypto";
import { jsonResponse, refuse } from "./feedback_http";
import { publicStatus, underReview } from "./feedback_read";
import { admit, firstBusyOfDay, refund } from "./feedback_quota";
import { capOverride, isBlocked } from "./feedback_blocks";
import { installLevel } from "./feedback_level";
import { challengePassed } from "./feedback_turnstile";
import { FeedbackSubmit, type FeedbackSubmitInput } from "./feedback_schema";
import {
  GLOBAL_DAILY,
  MAX_BODY_BYTES,
  MAX_REQUEST_BYTES,
  PER_IP_HOURLY,
  type FeedbackRow,
  type StoredAttachment,
} from "./feedback_types";
import { announce, type OpsWaiter } from "./ops_emit";
import { scrubSensitiveText } from "./scrub";

const RECEIPT_ATTEMPTS = 5;
const MAX_LINKS_BEFORE_HOLD = 3;

function scrubEnv(env: FeedbackSubmitInput["env"]): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(env)) if (typeof v === "string") out[k] = scrubSensitiveText(v);
  return out;
}

// A trusted install still goes through triage when its body is mostly links; a
// person describing a bug rarely pastes more than a few.
function tripsSpamGate(body: string): boolean {
  return (body.match(/https?:\/\//gi)?.length ?? 0) > MAX_LINKS_BEFORE_HOLD;
}

async function findByKey(env: Env, hash: string, key: string): Promise<FeedbackRow | null> {
  return env.DB.prepare("SELECT * FROM feedback WHERE install_hash = ? AND idempotency_key = ?").bind(hash, key).first<FeedbackRow>();
}

function receiptBody(row: FeedbackRow, token: string) {
  return {
    receipt: row.receipt,
    status: publicStatus(row),
    underReview: underReview(row),
    installToken: token,
    createdAt: row.created_at,
  };
}

async function insertWithReceipt(env: Env, row: Omit<FeedbackRow, "receipt">, key: string): Promise<FeedbackRow | { replay: FeedbackRow } | null> {
  for (let i = 0; i < RECEIPT_ATTEMPTS; i++) {
    const receipt = newReceipt();
    try {
      await env.DB.prepare(
        `INSERT INTO feedback (receipt, install_hash, idempotency_key, category, body, display_name, contact, env_json,
           attachments_json, status, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
      )
        .bind(receipt, row.install_hash, key, row.category, row.body, row.display_name, row.contact, row.env_json, row.attachments_json, row.status, row.created_at, row.updated_at)
        .run();
      return { ...row, receipt };
    } catch (err) {
      const existing = await findByKey(env, row.install_hash, key);
      if (existing) return { replay: existing };
      if (i === RECEIPT_ATTEMPTS - 1) throw err;
    }
  }
  return null;
}

async function storeAll(env: Env, decoded: ReturnType<typeof decodeAttachment>[]): Promise<StoredAttachment[]> {
  const stored: StoredAttachment[] = [];
  try {
    for (const [i, d] of decoded.entries()) if (d.ok && env.TELEMETRY_RAW) stored.push(await storeAttachment(env.TELEMETRY_RAW, d, i));
  } catch (err) {
    if (env.TELEMETRY_RAW) await deleteAttachments(env.TELEMETRY_RAW, stored);
    throw err;
  }
  return stored;
}

export async function handleSubmit(request: Request, env: Env, ctx?: OpsWaiter): Promise<Response> {
  const secret = env.FEEDBACK_TOKEN_SECRET;
  if (!feedbackEnabled(env) || !secret) return refuse("feedback.disabled", "feedback is unavailable");
  const text = await readCappedText(request, MAX_REQUEST_BYTES);
  if (text === null) return refuse("feedback.too_large", "request too large");
  let raw: unknown;
  try {
    raw = JSON.parse(text);
  } catch {
    return refuse("feedback.invalid", "body is not valid JSON");
  }
  const parsed = FeedbackSubmit.safeParse(raw);
  if (!parsed.success) return refuse("feedback.invalid", "request does not match the feedback schema");
  const input = parsed.data;
  if (new TextEncoder().encode(input.body).length > MAX_BODY_BYTES) return refuse("feedback.too_large", "body exceeds 8192 bytes");

  const hash = await installHash(secret, input.installId);
  const token = await installToken(secret, input.installId);
  const ip = request.headers.get("cf-connecting-ip") ?? "unknown";
  const ipKey = await ipHash(secret, ip);
  // Knowing the idempotency key proves the caller made the first attempt, so a
  // replay may recover the token even when the first response was lost.
  const replay = await findByKey(env, hash, input.idempotencyKey);
  if (replay) return jsonResponse(receiptBody(replay, token), 200);
  if ((await isKnownInstall(env, hash)) && !(await tokenMatches(secret, input.installId, request.headers.get("x-install-token") ?? ""))) {
    return refuse("feedback.bad_token", "install token missing or invalid");
  }
  // Only a new submission meets the block, after the replay and token answers a
  // blocked install gets exactly as an unblocked one would.
  const now = new Date();
  const level = await installLevel(env, hash, now);
  const limits = {
    trusted: level.trusted,
    globalDaily: (await capOverride(env)) ?? GLOBAL_DAILY,
    ipHourly: PER_IP_HOURLY,
    installHourly: level.limits.hourly,
    installDaily: level.limits.daily,
  };
  const blocked = await isBlocked(env, [`install:${hash}`, `ip:${ipKey}`], now);
  if (!(await challengePassed(env, input.turnstileToken, ip))) return refuse("feedback.challenge_required", "verification required");
  const decoded = input.attachments.map(decodeAttachment);
  for (const d of decoded) {
    if (!d.ok) {
      if (d.reason === "metadata") return refuse("feedback.image_metadata", "attachment carries metadata; re-encode the image");
      return refuse(d.reason === "too_large" ? "feedback.too_large" : "feedback.invalid", "attachment rejected");
    }
  }
  if (decoded.length > 0 && !env.TELEMETRY_RAW) return refuse("feedback.disabled", "attachments are unavailable");

  const admission = await admit(env, { ipKey, installHash: hash }, now, { kind: "submit", limits }, blocked, ip);
  if (admission.kind === "refused") {
    if (admission.limit === "global_daily") {
      console.error("feedback: global daily cap reached");
      if (await firstBusyOfDay(env, now)) await sendAlert(env, `Feedback global daily cap (${limits.globalDaily}) reached or reserved for trusted installs; submissions are refused until 00:00 UTC.`);
    }
    return admission.response;
  }
  let stored: StoredAttachment[] = [];
  const body = scrubSensitiveText(input.body);
  const at = new Date().toISOString();
  let outcome: Awaited<ReturnType<typeof insertWithReceipt>>;
  try {
    stored = await storeAll(env, decoded);
    outcome = await insertWithReceipt(
      env,
      {
        install_hash: hash,
        category: input.category,
        body,
        display_name: scrubSensitiveText(input.displayName),
        contact: input.contact ?? "",
        env_json: JSON.stringify(scrubEnv(input.env)),
        attachments_json: JSON.stringify(stored),
        status: level.heldEligible && !tripsSpamGate(body) ? "received" : "held",
        issue_number: null,
        issue_url: null,
        resolved_version: null,
        duplicate_of: null,
        created_at: at,
        updated_at: at,
      },
      input.idempotencyKey,
    );
  } catch (err) {
    await refund(env, admission.buckets);
    if (env.TELEMETRY_RAW) await deleteAttachments(env.TELEMETRY_RAW, stored);
    throw err;
  }
  if (!outcome || "replay" in outcome) {
    await refund(env, admission.buckets);
    if (env.TELEMETRY_RAW) await deleteAttachments(env.TELEMETRY_RAW, stored);
    if (!outcome) return refuse("feedback.disabled", "could not allocate a receipt");
    return jsonResponse(receiptBody(outcome.replay, token), 200);
  }
  announce(ctx, env, { t: "submitted", receipt: outcome.receipt, category: outcome.category, status: outcome.status });
  return jsonResponse(receiptBody(outcome, token), 201);
}
