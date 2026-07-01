// Anti-spam gate. Identity is verified upstream (id.reasonix.io); this layer
// decides whether a *verified* member may create content right now, using trust
// levels rather than per-post content scanning — so it stays cheap and the cost
// sits at the identity/first-post gate, not on every message.
import type { Member } from "./env";
import { HttpError } from "./identity";

// Trust rises with real participation; each level unlocks capabilities. New
// members (0) are deliberately limited so a throwaway account can't spam.
export const TRUST = { NEW: 0, BASIC: 1, MEMBER: 2, REGULAR: 3, LEADER: 4 } as const;

// Posts/day cap by trust — a soft brake on flooding from fresh accounts.
export function dailyPostCap(trust: number): number {
  if (trust <= TRUST.NEW) return 5;
  if (trust === TRUST.BASIC) return 20;
  if (trust === TRUST.MEMBER) return 60;
  return Infinity;
}

// Auto-hide a post once this many distinct members flag it, pending mod review.
export const AUTO_HIDE_FLAGS = 4;

const URL_RE = /\bhttps?:\/\/|\bwww\.|[a-z0-9-]+\.(com|net|io|org|cn|xyz|top|shop|vip)\b/i;

export function containsLink(body: string): boolean {
  return URL_RE.test(body);
}

// Throws an HttpError when the member may not post. `minTrust` is the category
// gate; `body` enables the new-member link block.
export function assertCanPost(member: Member, opts: { minTrust: number; body: string }): void {
  if (!member.emailVerified) {
    throw new HttpError(403, "email_unverified", "Confirm your email address before posting.");
  }
  if (member.silencedUntil && member.silencedUntil > new Date().toISOString()) {
    throw new HttpError(403, "silenced", "Your account is temporarily restricted from posting.");
  }
  if (isStaff(member)) return;
  if (member.trust < opts.minTrust) {
    throw new HttpError(403, "insufficient_trust", "You don't have access to post in this category yet.");
  }
  if (member.trust < TRUST.BASIC && containsLink(opts.body)) {
    throw new HttpError(403, "links_restricted", "New members can't post links yet — this unlocks once you've participated a little.");
  }
}

function isStaff(member: Member): boolean {
  return member.role === "admin" || member.role === "moderator";
}

// Whether a member's action is subject to the per-IP creation limiter. Staff and
// trusted members (regular+) skip it; new/basic members are always rate-limited.
export function rateLimited(member: Member): boolean {
  return !isStaff(member) && member.trust < TRUST.REGULAR;
}
