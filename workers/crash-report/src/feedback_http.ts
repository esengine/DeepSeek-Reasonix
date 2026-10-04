export type FeedbackCode =
  | "feedback.too_large"
  | "feedback.rate_limited"
  | "feedback.invalid"
  | "feedback.disabled"
  | "feedback.duplicate"
  | "feedback.bad_token"
  | "feedback.unauthorized"
  | "feedback.not_found"
  | "feedback.bad_transition"
  | "feedback.method_not_allowed"
  | "feedback.busy"
  | "feedback.image_metadata"
  | "feedback.reply_limit"
  | "feedback.not_replyable"
  | "feedback.challenge_required";

const STATUS: Record<FeedbackCode, number> = {
  "feedback.too_large": 413,
  "feedback.rate_limited": 429,
  "feedback.invalid": 400,
  "feedback.disabled": 503,
  "feedback.duplicate": 409,
  "feedback.bad_token": 401,
  "feedback.unauthorized": 401,
  "feedback.not_found": 404,
  "feedback.bad_transition": 409,
  "feedback.method_not_allowed": 405,
  "feedback.busy": 503,
  "feedback.image_metadata": 400,
  "feedback.reply_limit": 429,
  "feedback.not_replyable": 409,
  "feedback.challenge_required": 403,
};

export function jsonResponse(body: unknown, status = 200, headers: Record<string, string> = {}): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json; charset=utf-8", "cache-control": "no-store", "x-content-type-options": "nosniff", ...headers },
  });
}

export type FeedbackLimit = "ip_hourly" | "install_hourly" | "install_daily" | "reply_hourly" | "reply_item" | "admin_attempts" | "global_daily" | "global_burst";

export interface LimitDetails {
  limit: FeedbackLimit;
  resetsAt: string | null;
  retryAfterSeconds: number | null;
}

export function windowDetails(limit: FeedbackLimit, now: Date): LimitDetails {
  const span = limit === "global_daily" || limit === "install_daily" ? 86_400_000 : limit === "admin_attempts" ? 900_000 : limit === "global_burst" ? 60_000 : 3_600_000;
  const reset = limit === "reply_item" ? null : limit === "global_burst" ? now.getTime() + span : (Math.floor(now.getTime() / span) + 1) * span;
  return { limit, resetsAt: reset === null ? null : new Date(reset).toISOString(), retryAfterSeconds: reset === null ? null : Math.ceil((reset - now.getTime()) / 1000) };
}

export function refuse(code: "feedback.rate_limited" | "feedback.reply_limit", message: string, params: LimitDetails): Response;
export function refuse(code: Exclude<FeedbackCode, "feedback.rate_limited" | "feedback.reply_limit">, message: string, params?: LimitDetails): Response;
export function refuse(code: FeedbackCode, message: string, params?: LimitDetails): Response {
  const headers: Record<string, string> = params?.retryAfterSeconds === null || params?.retryAfterSeconds === undefined ? {} : { "retry-after": String(params.retryAfterSeconds) };
  return jsonResponse({ error: { code, message, ...(params ? { params } : {}) } }, STATUS[code], headers);
}
