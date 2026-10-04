export const CATEGORIES = ["bug", "idea", "question", "other"] as const;
export type Category = (typeof CATEGORIES)[number];

export const STATUSES = ["held", "needs_info", "answered", "rejected", "received", "recorded", "in_progress", "fixed", "wontfix", "duplicate"] as const;
export type Status = (typeof STATUSES)[number];

export const MAX_BODY_BYTES = 8192;
export const MAX_ATTACHMENT_BYTES = 2 * 1024 * 1024;
export const MAX_ATTACHMENTS = 3;
export const MAX_REQUEST_BYTES = 8 * 1024 * 1024;
export const PER_INSTALL_HOURLY = 3;
export const PER_IP_HOURLY = 10;
export const GLOBAL_DAILY = 300;
export const UNCONVERTED_RETENTION_DAYS = 30;
export const MAX_IMAGE_PIXELS = 40_000_000;
export const PER_INSTALL_DAILY = 10;
export const MAX_REPLY_BYTES = 4096;
export const MAX_REPLIES_PER_ITEM = 10;
export const REPLIES_PER_INSTALL_HOURLY = 3;
export const AUTO_BLOCK_REJECTIONS = 3;
export const AUTO_BLOCK_WINDOW_DAYS = 7;
export const AUTO_BLOCK_HOURS = 168;
export const TRUST_DAYS = 30;
export const TRUST_ESTABLISHED_DAYS = 90;
export const TRUST_ESTABLISHED_RELEASES = 5;
export const MANUAL_TRUST_DAYS = 365;
export const TRUSTED_PER_INSTALL_HOURLY = 12;
export const TRUSTED_PER_INSTALL_DAILY = 60;
export const TRUSTED_REPLIES_PER_INSTALL_HOURLY = 10;
export const RESERVED_SHARE = 0.1;
export const MAX_CAP = 5000;

export interface StoredAttachment {
  key: string;
  name: string;
  contentType: string;
  size: number;
}

export interface FeedbackRow {
  receipt: string;
  install_hash: string;
  category: Category;
  body: string;
  display_name: string;
  contact: string;
  env_json: string;
  attachments_json: string;
  status: Status;
  issue_number: number | null;
  issue_url: string | null;
  resolved_version: string | null;
  duplicate_of: string | null;
  created_at: string;
  updated_at: string;
}

// Rank orders the forward-only lifecycle of converted reports; the three outcomes
// share the top rank so none of them can move to another. Triage-only statuses
// never reach the converter and rank below everything it owns.
export function statusRank(s: Status): number {
  switch (s) {
    case "held":
    case "needs_info":
    case "answered":
    case "rejected":
    case "received":
      return 0;
    case "recorded":
      return 1;
    case "in_progress":
      return 2;
    default:
      return 3;
  }
}
