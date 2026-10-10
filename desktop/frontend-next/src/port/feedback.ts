// In-app feedback: a report the maintainers triage, the receipt that follows
// it, and the thread of replies under it. Field names are the kernel's json
// tags; a refusal is told apart by its code, never by its sentence.

export type FeedbackCategory = "bug" | "idea" | "question" | "other";

export const FEEDBACK_CATEGORIES: readonly FeedbackCategory[] = ["bug", "idea", "question", "other"];

// What a person may see. A report waiting for triage reads as received; one
// the maintainers declined reads as closed, with no reason.
export type FeedbackStatus =
  | "received" | "needs_info" | "answered" | "recorded" | "in_progress" | "fixed" | "wontfix" | "duplicate" | "closed";

export const FEEDBACK_STATUSES: readonly FeedbackStatus[] = [
  "received", "needs_info", "answered", "recorded", "in_progress", "fixed", "wontfix", "duplicate", "closed",
];

export const isUnderReview = (item: Pick<FeedbackItem, "status" | "underReview" | "statusUnavailable">): boolean =>
  item.status === "received" && item.underReview === true && !item.statusUnavailable;

// Only these take a reply from the reporter; the service has the last word.
export const FEEDBACK_REPLYABLE: readonly FeedbackStatus[] = ["needs_info", "answered", "recorded", "in_progress"];

export interface FeedbackEnvInfo {
  version: string;
  commit: string;
  surface: string;
  os: string;
  osVersion: string;
  arch: string;
  locale: string;
  channel: string;
  providerKind: string;
}

// uploadBytes bounds one raw file the page may hand over; the kernel shrinks
// and re-encodes it, and imageBytes is what is finally sent.
export interface FeedbackLimits {
  bodyBytes: number;
  nameChars: number;
  contactChars: number;
  images: number;
  imageBytes: number;
  uploadBytes: number;
  replyBytes: number;
}

export interface FeedbackEnv {
  env: FeedbackEnvInfo;
  displayName: string;
  limits: FeedbackLimits;
}

export interface FeedbackImage {
  name: string;
  dataBase64: string;
}

export interface FeedbackRequest {
  idempotencyKey: string;
  category: FeedbackCategory;
  body: string;
  displayName: string;
  contact?: string;
  // The UI language tag, so the issue can be triaged in the reporter's language.
  locale?: string;
  images: FeedbackImage[];
  // Answer to a feedback.challenge_required refusal.
  turnstileToken?: string;
}

export interface FeedbackReplyReceipt {
  replyId: number;
  createdAt: string;
}

export interface FeedbackReceipt {
  receipt: string;
  status: FeedbackStatus;
  createdAt: string;
  redacted: boolean;
  // A maintainer is reading the report before it is listed; status stays received.
  underReview: boolean;
}

export type FeedbackAuthor = "maintainer" | "user";

// Plain text only: never linked, formatted or interpreted.
export interface FeedbackReply {
  id: number;
  author: FeedbackAuthor;
  body: string;
  createdAt: string;
}

export interface FeedbackItem {
  receipt: string;
  category: FeedbackCategory;
  titleSnippet: string;
  status: FeedbackStatus;
  issueNumber?: number;
  issueUrl?: string;
  // "vX.Y.Z", or the literal "next" while the fix is merged and not yet shipped.
  resolvedVersion?: string;
  duplicateOf?: number;
  // The report was filed under an install identity this machine no longer holds.
  statusUnavailable?: boolean;
  // The maintainers are waiting for the reporter's answer.
  needsInput: boolean;
  // A received report a maintainer is reading; a service that omits it reads as false.
  underReview: boolean;
  // Oldest first.
  replies: FeedbackReply[];
  // Maintainer replies newer than the last one the kernel was told was seen.
  unreadReplies: number;
  createdAt: string;
  updatedAt: string;
}

// What the service admits from this install right now. It follows live trust,
// so it can sit below what the level alone would allow.
export interface FeedbackEffectiveLimits {
  reportsPerHour: number;
  reportsPerDay: number;
  repliesPerHour: number;
}

// Why the install is admitted at the limits it is. Only active and
// legacy_active raise them; a value the page does not know raises nothing.
export type FeedbackTrustState = "active" | "legacy_active" | "lapsed" | "revoked" | "none";

// The install's earned standing, derived by the service from shipped outcomes
// and never sent by the page. The thresholds ride with it, so the page holds no
// table. The next* fields and remaining are all null at the top level.
export interface FeedbackProfile {
  level: number;
  adoptedCount: number;
  currentThreshold: number;
  nextLevel: number | null;
  nextThreshold: number | null;
  remaining: number | null;
  trustState: FeedbackTrustState;
  trustExpiresAt: string | null;
  observedAt: string;
  effectiveLimits: FeedbackEffectiveLimits;
}

// offline: the service was unreachable, so items are the receipts remembered
// on this machine and their statuses may be stale; profile is then the last one
// the service confirmed.
// unread counts the reports wanting attention; hasNew is unread > 0.
// profile is null when the service stated none the kernel could trust.
export interface FeedbackMine {
  items: FeedbackItem[];
  offline: boolean;
  unread: number;
  hasNew: boolean;
  profile?: FeedbackProfile | null;
}

// The window a refusal came from, as the service names it. A refusal carries
// these in its params; an identifier not listed here is still a window.
export const FEEDBACK_LIMIT = {
  ipHourly: "ip_hourly",
  installHourly: "install_hourly",
  installDaily: "install_daily",
  replyHourly: "reply_hourly",
  replyItem: "reply_item",
  globalDaily: "global_daily",
  globalBurst: "global_burst",
} as const;

export const FEEDBACK_REPO_ISSUES = "https://github.com/esengine/DeepSeek-Reasonix/issues/";

export const FEEDBACK_NEXT_VERSION = "next";

export const FEEDBACK_CODE = {
  invalid: "feedback.invalid",
  tooLarge: "feedback.too_large",
  rateLimited: "feedback.rate_limited",
  busy: "feedback.busy",
  imageMetadata: "feedback.image_metadata",
  disabled: "feedback.disabled",
  duplicate: "feedback.duplicate",
  badToken: "feedback.bad_token",
  replyLimit: "feedback.reply_limit",
  notReplyable: "feedback.not_replyable",
  challengeRequired: "feedback.challenge_required",
  offline: "feedback.offline",
  unavailable: "feedback.unavailable",
  internal: "feedback.internal",
  badBody: "request.bad_body",
} as const;
