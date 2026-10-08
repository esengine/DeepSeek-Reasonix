import type { Env } from "./env";
import {
  PER_INSTALL_DAILY, PER_INSTALL_HOURLY, REPLIES_PER_INSTALL_HOURLY,
  TRUSTED_PER_INSTALL_DAILY, TRUSTED_PER_INSTALL_HOURLY, TRUSTED_REPLIES_PER_INSTALL_HOURLY,
} from "./feedback_types";

export interface LevelRow {
  threshold: number;
  hourly: number;
  daily: number;
  replies: number;
}

// The only threshold and quota table. L0 and L6 are the shipped untrusted and
// trusted tiers, so no level ever admits more than the reviewed trusted ceiling.
export const FEEDBACK_LEVELS: readonly LevelRow[] = [
  { threshold: 0, hourly: PER_INSTALL_HOURLY, daily: PER_INSTALL_DAILY, replies: REPLIES_PER_INSTALL_HOURLY },
  { threshold: 1, hourly: 5, daily: 15, replies: 4 },
  { threshold: 3, hourly: 6, daily: 20, replies: 5 },
  { threshold: 6, hourly: 8, daily: 25, replies: 6 },
  { threshold: 12, hourly: 10, daily: 30, replies: 8 },
  { threshold: 24, hourly: TRUSTED_PER_INSTALL_HOURLY, daily: 40, replies: TRUSTED_REPLIES_PER_INSTALL_HOURLY },
  { threshold: 48, hourly: TRUSTED_PER_INSTALL_HOURLY, daily: TRUSTED_PER_INSTALL_DAILY, replies: TRUSTED_REPLIES_PER_INSTALL_HOURLY },
];

const BASE = FEEDBACK_LEVELS[0];
const CEILING = FEEDBACK_LEVELS[FEEDBACK_LEVELS.length - 1];

export type TrustState = "active" | "legacy_active" | "lapsed" | "revoked" | "none";

export interface LevelSnapshot {
  installHash: string;
  adopted: number;
  ledgerRows: number;
  highWater: number;
  trustExpiresAt: string | null;
  revoked: boolean;
  blocked: boolean;
  grandfathered: boolean;
}

export interface FeedbackLevelResolution {
  level: number;
  adoptedCount: number;
  currentThreshold: number;
  nextLevel: number | null;
  nextThreshold: number | null;
  remaining: number | null;
  highWaterLevel: number;
  trustState: TrustState;
  trustExpiresAt: string | null;
  limits: { hourly: number; daily: number; replies: number };
  trusted: boolean;
  heldEligible: boolean;
}

function floorLimits(a: LevelRow, b: LevelRow): LevelRow {
  return { threshold: a.threshold, hourly: Math.max(a.hourly, b.hourly), daily: Math.max(a.daily, b.daily), replies: Math.max(a.replies, b.replies) };
}

function levelFor(count: number): number {
  let level = 0;
  FEEDBACK_LEVELS.forEach((row, i) => {
    if (count >= row.threshold) level = i;
  });
  return level;
}

// One read, so the count, trust, revocation and block state come from the same
// instant. Only server records are consulted, never anything the caller sent.
export async function levelSnapshot(env: Env, installHash: string, now: Date): Promise<LevelSnapshot> {
  const at = now.toISOString();
  const r = await env.DB.prepare(
    `SELECT
       (SELECT COUNT(*) FROM feedback_adoptions WHERE install_hash = ? AND tombstoned_at IS NULL) AS adopted,
       (SELECT COUNT(*) FROM feedback_adoptions WHERE install_hash = ?) AS ledger_rows,
       (SELECT high_water_count FROM feedback_level_state WHERE install_hash = ?) AS high_water,
       (SELECT expires_at FROM feedback_trust WHERE install_hash = ?) AS trust_expires,
       (SELECT revoked_at FROM feedback_level_state WHERE install_hash = ?) AS revoked_at,
       (SELECT created_at < (SELECT MIN(adopted_at) FROM feedback_adoptions WHERE install_hash = ?) FROM feedback_trust WHERE install_hash = ?) AS grandfathered,
       EXISTS (SELECT 1 FROM feedback_blocks WHERE target = ? AND (expires_at IS NULL OR expires_at > ?)) AS blocked`,
  ).bind(installHash, installHash, installHash, installHash, installHash, installHash, installHash, `install:${installHash}`, at)
    .first<{ adopted: number; ledger_rows: number; high_water: number | null; trust_expires: string | null; revoked_at: string | null; grandfathered: number | null; blocked: number }>();
  return {
    installHash,
    adopted: r?.adopted ?? 0,
    ledgerRows: r?.ledger_rows ?? 0,
    highWater: r?.high_water ?? 0,
    trustExpiresAt: r?.trust_expires ?? null,
    revoked: (r?.revoked_at ?? null) !== null,
    blocked: (r?.blocked ?? 0) !== 0,
    grandfathered: (r?.grandfathered ?? 0) !== 0,
  };
}

// Earned level and privilege are separate: the level follows active credits, the
// quota follows live trust. Lapse and revocation keep the level but admit at L0.
// Only an install that never held a ledger row is legacy; a correction cannot raise limits.
// Trust that predates the first credit keeps the trusted tier as a floor, so recognition
// never narrows what the install already had; the level rows only ever add to a later grant.
export function resolveFeedbackLevel(installHash: string, snap: LevelSnapshot, now: Date): FeedbackLevelResolution {
  if (snap.installHash !== installHash) throw new Error("level snapshot belongs to another install");
  const level = levelFor(snap.adopted);
  const row = FEEDBACK_LEVELS[level];
  const next = FEEDBACK_LEVELS[level + 1];
  const live = snap.trustExpiresAt !== null && snap.trustExpiresAt > now.toISOString();
  const trustState: TrustState = live ? (snap.ledgerRows > 0 ? "active" : "legacy_active") : snap.revoked ? "revoked" : snap.adopted > 0 ? "lapsed" : "none";
  const limits = trustState === "active" ? (snap.grandfathered ? floorLimits(row, CEILING) : row) : trustState === "legacy_active" ? CEILING : BASE;
  return {
    level,
    adoptedCount: snap.adopted,
    currentThreshold: row.threshold,
    nextLevel: next ? level + 1 : null,
    nextThreshold: next ? next.threshold : null,
    remaining: next ? next.threshold - snap.adopted : null,
    highWaterLevel: Math.max(level, levelFor(snap.highWater)),
    trustState,
    trustExpiresAt: snap.trustExpiresAt,
    limits: { hourly: limits.hourly, daily: limits.daily, replies: limits.replies },
    trusted: live,
    heldEligible: live && !snap.blocked,
  };
}

export async function installLevel(env: Env, installHash: string, now: Date): Promise<FeedbackLevelResolution> {
  return resolveFeedbackLevel(installHash, await levelSnapshot(env, installHash, now), now);
}

export function levelProfile(r: FeedbackLevelResolution, now: Date) {
  return {
    level: r.level,
    adoptedCount: r.adoptedCount,
    currentThreshold: r.currentThreshold,
    nextLevel: r.nextLevel,
    nextThreshold: r.nextThreshold,
    remaining: r.remaining,
    trustState: r.trustState,
    trustExpiresAt: r.trustExpiresAt,
    observedAt: now.toISOString(),
    effectiveLimits: { reportsPerHour: r.limits.hourly, reportsPerDay: r.limits.daily, repliesPerHour: r.limits.replies },
  };
}
