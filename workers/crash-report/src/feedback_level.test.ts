import { describe, expect, it } from "vitest";
import { FEEDBACK_LEVELS, resolveFeedbackLevel, type LevelSnapshot } from "./feedback_level";
import { PER_INSTALL_DAILY, PER_INSTALL_HOURLY, REPLIES_PER_INSTALL_HOURLY, TRUSTED_PER_INSTALL_DAILY, TRUSTED_PER_INSTALL_HOURLY, TRUSTED_REPLIES_PER_INSTALL_HOURLY } from "./feedback_types";

const NOW = new Date("2026-10-03T12:00:00.000Z");
const FUTURE = "2026-10-20T00:00:00.000Z";
const PAST = "2026-10-01T00:00:00.000Z";
const H = "a".repeat(64);
const snap = (over: Partial<LevelSnapshot> = {}): LevelSnapshot => ({
  installHash: H, adopted: 0, ledgerRows: over.adopted ?? 0, highWater: 0, trustExpiresAt: null, revoked: false, blocked: false, ...over,
});
const resolve = (over: Partial<LevelSnapshot> = {}) => resolveFeedbackLevel(H, snap(over), NOW);

describe("level table", () => {
  it("anchors L0 and L6 to the shipped untrusted and trusted tiers", () => {
    expect(FEEDBACK_LEVELS[0]).toMatchObject({ threshold: 0, hourly: PER_INSTALL_HOURLY, daily: PER_INSTALL_DAILY, replies: REPLIES_PER_INSTALL_HOURLY });
    expect(FEEDBACK_LEVELS[6]).toMatchObject({ threshold: 48, hourly: TRUSTED_PER_INSTALL_HOURLY, daily: TRUSTED_PER_INSTALL_DAILY, replies: TRUSTED_REPLIES_PER_INSTALL_HOURLY });
    expect(FEEDBACK_LEVELS.map((l) => [l.threshold, l.hourly, l.daily, l.replies])).toEqual([
      [0, 3, 10, 3], [1, 5, 15, 4], [3, 6, 20, 5], [6, 8, 25, 6], [12, 10, 30, 8], [24, 12, 40, 10], [48, 12, 60, 10],
    ]);
  });

  it("never exceeds the shipped trusted ceilings at any level", () => {
    for (const l of FEEDBACK_LEVELS) {
      expect(l.hourly).toBeLessThanOrEqual(TRUSTED_PER_INSTALL_HOURLY);
      expect(l.daily).toBeLessThanOrEqual(TRUSTED_PER_INSTALL_DAILY);
      expect(l.replies).toBeLessThanOrEqual(TRUSTED_REPLIES_PER_INSTALL_HOURLY);
    }
  });
});

describe("resolveFeedbackLevel", () => {
  it.each([
    [0, 0, 1, 1], [1, 1, 3, 2], [2, 1, 3, 1], [3, 2, 6, 3], [5, 2, 6, 1], [6, 3, 12, 6], [11, 3, 12, 1],
    [12, 4, 24, 12], [23, 4, 24, 1], [24, 5, 48, 24], [47, 5, 48, 1], [48, 6, null, null], [500, 6, null, null],
  ])("count %i is level %i with next threshold %s and %s remaining", (count, level, next, remaining) => {
    const r = resolve({ adopted: count, trustExpiresAt: FUTURE });
    expect(r.level).toBe(level);
    expect(r.adoptedCount).toBe(count);
    expect(r.nextThreshold).toBe(next);
    expect(r.remaining).toBe(remaining);
    expect(r.nextLevel).toBe(next === null ? null : level + 1);
    expect(r.currentThreshold).toBe(FEEDBACK_LEVELS[level].threshold);
  });

  it("uses the table row for active trust with credit", () => {
    const r = resolve({ adopted: 3, trustExpiresAt: FUTURE });
    expect(r).toMatchObject({ trustState: "active", trusted: true, heldEligible: true, limits: { hourly: 6, daily: 20, replies: 5 } });
  });

  it("keeps the shipped trusted limits for trust without credit until it expires", () => {
    const r = resolve({ trustExpiresAt: FUTURE });
    expect(r).toMatchObject({ level: 0, trustState: "legacy_active", trusted: true, limits: { hourly: 12, daily: 60, replies: 10 } });
    expect(resolve({ trustExpiresAt: PAST })).toMatchObject({ trustState: "none", trusted: false, limits: { hourly: 3, daily: 10, replies: 3 } });
  });

  it("never maps a fully tombstoned ledger back to the legacy tier", () => {
    const r = resolve({ adopted: 0, ledgerRows: 2, trustExpiresAt: FUTURE });
    expect(r).toMatchObject({ level: 0, trustState: "active", trusted: true, limits: { hourly: 3, daily: 10, replies: 3 } });
  });

  it("keeps level, count and progress on lapse but admits at L0 ceilings", () => {
    const r = resolve({ adopted: 7, trustExpiresAt: PAST });
    expect(r).toMatchObject({ level: 3, adoptedCount: 7, nextThreshold: 12, remaining: 5, trustState: "lapsed", trusted: false, heldEligible: false, limits: { hourly: 3, daily: 10, replies: 3 } });
    expect(resolve({ adopted: 7 }).trustState).toBe("lapsed");
  });

  it("admits a revoked install at L0 ceilings while keeping its count", () => {
    const r = resolve({ adopted: 25, revoked: true });
    expect(r).toMatchObject({ level: 5, trustState: "revoked", trusted: false, limits: { hourly: 3, daily: 10, replies: 3 } });
  });

  it("lets live trust win over a stale revocation marker", () => {
    expect(resolve({ adopted: 1, revoked: true, trustExpiresAt: FUTURE }).trustState).toBe("active");
  });

  it("withholds the held bypass from a blocked install without changing its limits", () => {
    const r = resolve({ adopted: 6, trustExpiresAt: FUTURE, blocked: true });
    expect(r).toMatchObject({ heldEligible: false, trusted: true, limits: { hourly: 8, daily: 25, replies: 6 } });
  });

  it("reports the high-water level separately from the current level", () => {
    const r = resolve({ adopted: 2, highWater: 7, trustExpiresAt: FUTURE });
    expect(r).toMatchObject({ level: 1, highWaterLevel: 3 });
    expect(resolve({ adopted: 9, highWater: 2 }).highWaterLevel).toBe(3);
  });

  it("reads trust expiry at the boundary as lapsed", () => {
    expect(resolve({ adopted: 1, trustExpiresAt: NOW.toISOString() }).trustState).toBe("lapsed");
  });

  it("refuses a snapshot that belongs to another install", () => {
    expect(() => resolveFeedbackLevel("b".repeat(64), snap(), NOW)).toThrow();
  });
});
