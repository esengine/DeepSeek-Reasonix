// @ts-expect-error Node 22+ provides node:sqlite for boundary tests.
import { DatabaseSync } from "node:sqlite";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Env } from "./env";
import { ipHash } from "./feedback_crypto";
import { purgeStaleFeedback } from "./feedback_retention";
import { handleFeedbackRoute } from "./feedback_routes";
import { d1 } from "./feedback_testkit";
import adoptionsSQL from "../migrate-feedback-adoptions.sql?raw";
import migration from "../migrate-feedback.sql?raw";
import triage from "../migrate-feedback-triage.sql?raw";

const NOW = "2026-10-03T12:34:56.000Z";
const HOUR = "2026-10-03T13:00:00.000Z";
const admin = { authorization: "Bearer fixture-admin" };
const IDS = ["install-aaaaaaaaaaaaaaaa", "install-bbbbbbbbbbbbbbbb", "install-cccccccccccccccc"];
const KEY = (n: number) => `esengine/deepseek-reasonix#${n}`;
let db: DatabaseSync;
let env: Env;
let ipAllowed: boolean;
let seq = 0;
const tokens = new Map<string, string>();
const hashes = new Map<string, string>();

const call = async (path: string, init: { method?: string; body?: unknown; headers?: Record<string, string> } = {}) =>
  await handleFeedbackRoute(new Request(`https://crash.test${path}`, {
    method: init.method ?? (init.body === undefined ? "GET" : "POST"),
    headers: init.headers ?? admin,
    ...(init.body === undefined ? {} : { body: JSON.stringify(init.body) }),
  }), env) as Response;
const as = (id: string) => ({ "x-install-id": id, "x-install-token": tokens.get(id) ?? "" });
const act = (receipt: string, action: string, body: unknown = {}, method = "POST") => call(`/v1/admin/feedback/${receipt}/${action}`, { body: method === "DELETE" ? undefined : body, method });
const json = async (r: Response) => await r.json() as any;
const mine = async (id: string) => json(await call("/v1/feedback/mine", { headers: as(id) }));
const row = <T>(sql: string, ...args: unknown[]) => db.prepare(sql).get(...args) as T;
const rows = <T>(sql: string, ...args: unknown[]) => db.prepare(sql).all(...args) as T[];
const count = (sql: string, ...args: unknown[]) => (row<{ n: number }>(`SELECT COUNT(*) AS n FROM (${sql})`, ...args)).n;
const adoptions = (id = IDS[0]) => count("SELECT 1 FROM feedback_adoptions WHERE install_hash = ? AND tombstoned_at IS NULL", hashes.get(id));
const auditCount = (action: string) => count("SELECT 1 FROM feedback_audit WHERE action = ?", action);

async function submit(id: string, extra: Record<string, unknown> = {}): Promise<Response> {
  db.exec("DELETE FROM feedback_quota");
  const res = await call("/v1/feedback", {
    headers: { "x-install-token": tokens.get(id) ?? "" },
    body: { installId: id, idempotencyKey: `fixture-key-${++seq}`, body: "Neutral fixture", category: "idea", displayName: "Fixture", env: {}, ...extra },
  });
  const j = res.clone();
  if (res.status === 201) {
    const b = await j.json() as any;
    tokens.set(id, b.installToken);
    if (!hashes.has(id)) hashes.set(id, row<{ install_hash: string }>("SELECT install_hash FROM feedback WHERE receipt = ?", b.receipt).install_hash);
  }
  return res;
}

const send = (id: string, key: string, extra: Record<string, unknown> = {}, headers: Record<string, string> = {}) =>
  call("/v1/feedback", {
    headers: { "x-install-token": tokens.get(id) ?? "", ...headers },
    body: { installId: id, idempotencyKey: key, body: "Neutral fixture", category: "idea", displayName: "Fixture", env: {}, ...extra },
  });
const stored = (receipt: string) => row<{ status: string }>("SELECT status FROM feedback WHERE receipt = ?", receipt).status;

// Submits, releases and links a report to an issue; stops before any status change.
async function recorded(id: string, issue: number, released = true): Promise<string> {
  const receipt = (await json(await submit(id))).receipt as string;
  if (released) expect((await act(receipt, "release")).status).toBe(200);
  expect((await act(receipt, "link", { issueNumber: issue })).status).toBe(200);
  return receipt;
}
const fix = (receipt: string, version = "v1.2.3") => act(receipt, "status", { status: "fixed", resolvedVersion: version });
async function shipped(id: string, issue: number, version = "v1.2.3", released = true): Promise<string> {
  const receipt = await recorded(id, issue, released);
  expect((await fix(receipt, version)).status).toBe(200);
  return receipt;
}
// Fixture state only: ledger rows for quota tests, never produced by a client.
function credit(id: string, n: number, tombstoned = 0) {
  const h = hashes.get(id);
  for (let i = 0; i < n + tombstoned; i++) {
    db.prepare("INSERT INTO feedback_adoptions (install_hash, item_key, receipt, version, adopted_at, tombstoned_at) VALUES (?,?,?,?,?,?)")
      .run(h, `fixture/repo#${1000 + i}`, `FB-FIX0-${String(i).padStart(4, "0")}`, "v1.0.0", NOW, i >= n ? NOW : null);
  }
}
const trust = (id: string, days = 30) => db.prepare("INSERT OR REPLACE INTO feedback_trust VALUES (?,?,?)").run(hashes.get(id), NOW, new Date(Date.parse(NOW) + days * 86400000).toISOString());
const block = (id: string) => db.prepare("INSERT OR REPLACE INTO feedback_blocks VALUES (?,?,?,NULL)").run(`install:${hashes.get(id)}`, "fixture", NOW);
const bucket = (prefix: string, id: string, n: number) =>
  db.prepare("INSERT OR REPLACE INTO feedback_quota VALUES (?,?,?)").run(`${prefix}:${hashes.get(id)}:${prefix === "id" ? NOW.slice(0, 10) : NOW.slice(0, 13)}`, n, NOW.slice(0, 10));

beforeEach(async () => {
  vi.useFakeTimers();
  vi.setSystemTime(NOW);
  db = new DatabaseSync(":memory:");
  db.exec(migration);
  db.exec(triage);
  db.exec(adoptionsSQL);
  ipAllowed = true;
  tokens.clear();
  hashes.clear();
  env = { DB: d1(db), FEEDBACK_ENABLED: "true", FEEDBACK_TOKEN_SECRET: "fixture-secret", FEEDBACK_ADMIN_TOKEN: "fixture-admin",
    FEEDBACK_LIMITER: { limit: async () => ({ success: ipAllowed }) } } as unknown as Env;
  expect((await submit(IDS[0])).status).toBe(201);
  db.exec("DELETE FROM feedback; DELETE FROM feedback_quota");
});
afterEach(() => { vi.useRealTimers(); db.close(); });

describe("migration", () => {
  it("is additive and idempotent, and preserves rows on rerun", () => {
    credit(IDS[0], 2);
    db.exec(adoptionsSQL);
    db.exec(adoptionsSQL);
    expect(adoptions()).toBe(2);
    expect(rows<{ name: string }>("SELECT name FROM sqlite_master WHERE name IN ('feedback_adoptions','feedback_adoptions_install','feedback_level_state')")).toHaveLength(3);
  });

  it("enforces one credit per install and item and one per receipt", () => {
    const h = hashes.get(IDS[0]);
    const ins = (key: string, receipt: string) => db.prepare("INSERT INTO feedback_adoptions (install_hash, item_key, receipt, version, adopted_at) VALUES (?,?,?,?,?)").run(h, key, receipt, "v1.0.0", NOW);
    ins("a/b#1", "FB-AAAA-AAAA");
    expect(() => ins("a/b#1", "FB-AAAA-BBBB")).toThrow();
    expect(() => ins("a/b#2", "FB-AAAA-AAAA")).toThrow();
  });
});

describe("crediting a shipped outcome", () => {
  it("credits once on fixed with a concrete version and renews trust in the same transaction", async () => {
    const receipt = await recorded(IDS[0], 4242);
    const res = await fix(receipt);
    expect(res.status).toBe(200);
    expect(await json(res)).toEqual({ receipt, status: "fixed", adoption: "credited" });
    expect(rows("SELECT item_key, receipt, version, tombstoned_at FROM feedback_adoptions")).toEqual([{ item_key: KEY(4242), receipt, version: "v1.2.3", tombstoned_at: null }]);
    expect(row<any>("SELECT high_water_count FROM feedback_level_state").high_water_count).toBe(1);
    expect(auditCount("adopt")).toBe(1);
    expect(row<any>("SELECT expires_at FROM feedback_trust").expires_at).toBe("2026-11-02T12:34:56.000Z");
  });

  it("is a no-op on replay, retry and later conflicting versions", async () => {
    const receipt = await shipped(IDS[0], 7);
    const before = rows("SELECT * FROM feedback_adoptions");
    for (let i = 0; i < 3; i++) expect((await fix(receipt)).status).toBe(200);
    expect((await fix(receipt, "v1.2.4")).status).toBe(409);
    expect(rows("SELECT * FROM feedback_adoptions")).toEqual(before);
    expect(auditCount("adopt")).toBe(1);
    expect(row<any>("SELECT high_water_count FROM feedback_level_state").high_water_count).toBe(1);
  });

  it("does not credit fixed with next, and credits once when next becomes concrete", async () => {
    const receipt = await recorded(IDS[0], 8);
    expect(await json(await fix(receipt, "next"))).toEqual({ receipt, status: "fixed" });
    expect(adoptions()).toBe(0);
    expect(await json(await fix(receipt, "next"))).toEqual({ receipt, status: "fixed" });
    expect(await json(await fix(receipt, "v2.0.0"))).toEqual({ receipt, status: "fixed", adoption: "credited" });
    expect(await json(await fix(receipt, "v2.0.0"))).toEqual({ receipt, status: "fixed" });
    expect(adoptions()).toBe(1);
  });

  it.each(["latest", "1.2.3", "v1.2", "v1.2.3-rc1"])("does not credit a non-concrete version %s", async (version) => {
    const receipt = await recorded(IDS[0], 9);
    expect((await fix(receipt, version)).status).toBe(200);
    expect(adoptions()).toBe(0);
  });

  it.each([["in_progress", {}], ["wontfix", {}], ["duplicate", { duplicateOf: "FB-AAAA-AAAA" }]])("never credits %s", async (status, extra) => {
    const receipt = await recorded(IDS[0], 10);
    expect((await act(receipt, "status", { status, ...extra })).status).toBe(200);
    expect(adoptions()).toBe(0);
  });

  it("never credits rejection, held release, replies, trust renewal or the release ledger", async () => {
    const a = (await json(await submit(IDS[0]))).receipt as string;
    const b = (await json(await submit(IDS[0], { category: "bug" }))).receipt as string;
    expect((await act(a, "release")).status).toBe(200);
    expect(count("SELECT 1 FROM feedback_releases")).toBe(1);
    expect((await act(b, "reject", { reason: "fixture" })).status).toBe(200);
    expect((await act(a, "trust")).status).toBe(200);
    expect((await act(a, "reply", { body: "answer" })).status).toBe(200);
    expect(count("SELECT 1 FROM feedback_adoptions")).toBe(0);
  });

  it("does not credit a report that never got an issue key", async () => {
    const receipt = (await json(await submit(IDS[0]))).receipt as string;
    db.prepare("UPDATE feedback SET status = 'in_progress' WHERE receipt = ?").run(receipt);
    expect(await json(await fix(receipt))).toEqual({ receipt, status: "fixed", adoption: "no_item_key" });
    expect(adoptions()).toBe(0);
  });

  it("derives the item key from the stored issue, never from the request", async () => {
    const receipt = await recorded(IDS[0], 55);
    await act(receipt, "status", { status: "fixed", resolvedVersion: "v1.0.0", itemKey: "evil/repo#1", installHash: hashes.get(IDS[1]), install_hash: hashes.get(IDS[1]) });
    expect(rows("SELECT install_hash, item_key FROM feedback_adoptions")).toEqual([{ install_hash: hashes.get(IDS[0]), item_key: KEY(55) }]);
  });

  it("credits a converter-recorded issue by its own repository", async () => {
    const receipt = (await json(await submit(IDS[0]))).receipt as string;
    await act(receipt, "release");
    await act(receipt, "recorded", { issueNumber: 31, issueUrl: "https://github.com/Other/Repo/issues/31" });
    await fix(receipt);
    expect(row<any>("SELECT item_key FROM feedback_adoptions").item_key).toBe("other/repo#31");
  });

  it("credits nothing when the stored issue url disagrees with the issue number", async () => {
    const receipt = (await json(await submit(IDS[0]))).receipt as string;
    await act(receipt, "release");
    await act(receipt, "recorded", { issueNumber: 31, issueUrl: "https://example.com/issues/31" });
    expect((await json(await fix(receipt))).adoption).toBe("no_item_key");
  });
});

describe("duplicates", () => {
  it("credits one reporter per canonical item", async () => {
    const first = await recorded(IDS[0], 100);
    const second = await recorded(IDS[1], 100);
    await fix(first);
    expect(await json(await fix(second))).toEqual({ receipt: second, status: "fixed", adoption: "item_credited" });
    expect(adoptions(IDS[0])).toBe(1);
    expect(adoptions(IDS[1])).toBe(0);
  });

  it("credits an install once for two receipts of the same item", async () => {
    const first = await recorded(IDS[0], 101);
    const second = await recorded(IDS[0], 101);
    await fix(first);
    expect((await json(await fix(second))).adoption).toBe("item_credited");
    expect(adoptions()).toBe(1);
  });

  it("lets a maintainer record an independent outcome explicitly", async () => {
    const first = await recorded(IDS[0], 102);
    const second = await recorded(IDS[1], 102);
    await fix(first);
    await fix(second);
    const res = await act(second, "adoption", { version: "v1.2.3" });
    expect(res.status).toBe(200);
    expect(await json(res)).toEqual({ receipt: second, adopted: true, credited: true });
    expect(adoptions(IDS[1])).toBe(1);
    expect(auditCount("adopt")).toBe(2);
    const again = await act(second, "adoption", { version: "v1.2.3" });
    expect(await json(again)).toEqual({ receipt: second, adopted: true, credited: false });
    expect(adoptions(IDS[1])).toBe(1);
  });
});

describe("explicit adoption record", () => {
  it("imports an already fixed report once and rejects unshipped or malformed input", async () => {
    const receipt = await recorded(IDS[0], 200);
    expect((await act(receipt, "adoption", { version: "v1.0.0" })).status).toBe(409);
    db.prepare("UPDATE feedback SET status = 'fixed', resolved_version = 'v1.0.0' WHERE receipt = ?").run(receipt);
    for (const body of [{}, { version: "next" }, { version: "v1.0.0", extra: 1 }, { version: "v9.9.9" }]) {
      expect([400, 409]).toContain((await act(receipt, "adoption", body)).status);
    }
    expect(adoptions()).toBe(0);
    expect((await act(receipt, "adoption", { version: "v1.0.0" })).status).toBe(200);
    expect((await act(receipt, "adoption", { version: "v1.0.0" })).status).toBe(200);
    expect(adoptions()).toBe(1);
    expect(auditCount("adopt")).toBe(1);
  });

  it("requires admin authentication and an allowed method, and rejects unknown receipts", async () => {
    const receipt = await shipped(IDS[0], 201);
    for (const method of ["POST", "DELETE"]) {
      const none = await call(`/v1/admin/feedback/${receipt}/adoption`, { method, headers: {} });
      expect(none.status).toBe(401);
      expect((await call(`/v1/admin/feedback/${receipt}/adoption`, { method, headers: { authorization: "Bearer wrong" } })).status).toBe(401);
      expect((await call(`/v1/admin/feedback/${receipt}/adoption`, { method, headers: as(IDS[0]) })).status).toBe(401);
    }
    for (const method of ["GET", "PUT", "PATCH"]) {
      const r = await call(`/v1/admin/feedback/${receipt}/adoption`, { method });
      expect(r.status).toBe(405);
      expect((await json(r)).error.code).toBe("feedback.method_not_allowed");
    }
    expect((await act("FB-ZZZZ-ZZZZ", "adoption", { version: "v1.0.0" })).status).toBe(404);
    expect((await act("FB-ZZZZ-ZZZZ", "adoption", {}, "DELETE")).status).toBe(404);
    expect(adoptions()).toBe(1);
  });
});

describe("tombstones and corrections", () => {
  it("excludes a tombstoned credit from the count, never deletes it and never lets replay re-credit it", async () => {
    const receipt = await shipped(IDS[0], 300);
    await shipped(IDS[0], 301);
    expect((await mine(IDS[0])).profile.level).toBe(1);
    const res = await act(receipt, "adoption", {}, "DELETE");
    expect(await json(res)).toEqual({ receipt, adopted: false, tombstoned: true });
    expect(adoptions()).toBe(1);
    expect(count("SELECT 1 FROM feedback_adoptions")).toBe(2);
    expect(row<any>("SELECT tombstoned_at FROM feedback_adoptions WHERE receipt = ?", receipt).tombstoned_at).not.toBeNull();
    expect(auditCount("adoption_tombstone")).toBe(1);
    expect((await json(await act(receipt, "adoption", {}, "DELETE"))).tombstoned).toBe(true);
    expect(auditCount("adoption_tombstone")).toBe(1);
    expect(await act(receipt, "adoption", { version: "v1.2.3" })).toHaveProperty("status", 409);
    expect((await json(await act(receipt, "adoption", { version: "v1.2.3" }))).error.code).toBe("feedback.adoption_tombstoned");
    await fix(receipt);
    expect(adoptions()).toBe(1);
    const other = await recorded(IDS[1], 300);
    expect((await json(await fix(other))).adoption).toBe("tombstoned");
    expect(adoptions(IDS[1])).toBe(0);
  });

  it("lowers the level only through a correction and keeps the high-water mark", async () => {
    const receipts: string[] = [];
    for (let i = 0; i < 3; i++) receipts.push(await shipped(IDS[0], 400 + i));
    expect((await mine(IDS[0])).profile).toMatchObject({ level: 2, adoptedCount: 3 });
    await act(receipts[2], "adoption", {}, "DELETE");
    expect((await mine(IDS[0])).profile).toMatchObject({ level: 1, adoptedCount: 2 });
    expect(row<any>("SELECT high_water_count FROM feedback_level_state").high_water_count).toBe(3);
    vi.setSystemTime(new Date(Date.parse(NOW) + 200 * 86400000));
    expect((await mine(IDS[0])).profile).toMatchObject({ level: 1, adoptedCount: 2 });
  });
});

describe("corrections never raise limits", () => {
  const limits = async () => (await mine(IDS[0])).profile.effectiveLimits;

  it("keeps the effective quota from rising when the only credit is tombstoned", async () => {
    const receipt = await shipped(IDS[0], 950);
    expect(await limits()).toEqual({ reportsPerHour: 5, reportsPerDay: 15, repliesPerHour: 4 });
    await act(receipt, "adoption", {}, "DELETE");
    expect((await mine(IDS[0])).profile).toMatchObject({ level: 0, adoptedCount: 0, trustState: "active" });
    expect(await limits()).toEqual({ reportsPerHour: 3, reportsPerDay: 10, repliesPerHour: 3 });
  });

  it("applies the same ceiling to a manual 365-day grant after the correction", async () => {
    const receipt = await shipped(IDS[0], 951);
    await act(receipt, "adoption", {}, "DELETE");
    expect((await act(receipt, "trust")).status).toBe(200);
    expect(await limits()).toEqual({ reportsPerHour: 3, reportsPerDay: 10, repliesPerHour: 3 });
    bucket("ih", IDS[0], 3);
    expect((await send(IDS[0], "tombstoned-key-1")).status).toBe(429);
  });
});

describe("level read on mine", () => {
  it("reports the server-derived level and exactly the documented fields", async () => {
    await shipped(IDS[0], 500);
    const body = await mine(IDS[0]);
    expect(body.profile).toEqual({
      level: 1, adoptedCount: 1, currentThreshold: 1, nextLevel: 2, nextThreshold: 3, remaining: 2,
      trustState: "active", trustExpiresAt: "2026-11-02T12:34:56.000Z", observedAt: NOW,
      effectiveLimits: { reportsPerHour: 5, reportsPerDay: 15, repliesPerHour: 4 },
    });
    expect(body.items).toHaveLength(1);
  });

  it("shows a fresh install at L0 without exposing any hash or block state", async () => {
    await submit(IDS[1]);
    block(IDS[1]);
    const blocked = await mine(IDS[1]);
    expect(blocked.profile).toEqual({
      level: 0, adoptedCount: 0, currentThreshold: 0, nextLevel: 1, nextThreshold: 1, remaining: 1,
      trustState: "none", trustExpiresAt: null, observedAt: NOW, effectiveLimits: { reportsPerHour: 3, reportsPerDay: 10, repliesPerHour: 3 },
    });
    expect(JSON.stringify(blocked)).not.toContain(hashes.get(IDS[1]));
    db.exec("DELETE FROM feedback_blocks");
    expect((await mine(IDS[1])).profile).toEqual(blocked.profile);
  });

  it("keeps the badge of a lapsed install while reporting L0 limits", async () => {
    credit(IDS[0], 7);
    db.prepare("INSERT INTO feedback_trust VALUES (?,?,?)").run(hashes.get(IDS[0]), NOW, "2026-10-01T00:00:00.000Z");
    expect((await mine(IDS[0])).profile).toMatchObject({ level: 3, adoptedCount: 7, nextThreshold: 12, remaining: 5, trustState: "lapsed", effectiveLimits: { reportsPerHour: 3, reportsPerDay: 10, repliesPerHour: 3 } });
  });

  it("ignores any level a client claims and keeps the existing authentication", async () => {
    const claimed = await call("/v1/feedback/mine?level=6&count=99", { headers: { ...as(IDS[0]), "x-feedback-level": "6" } });
    expect((await json(claimed)).profile.level).toBe(0);
    expect((await call("/v1/feedback/mine", { headers: { "x-install-id": IDS[0], "x-install-token": "wrong" } })).status).toBe(401);
    expect((await call("/v1/feedback/mine", { method: "POST", body: {} })).status).toBe(405);
  });

  it("reports a legacy trusted install without credit as L0 with the shipped limits", async () => {
    trust(IDS[0]);
    expect((await mine(IDS[0])).profile).toMatchObject({ level: 0, trustState: "legacy_active", effectiveLimits: { reportsPerHour: 12, reportsPerDay: 60, repliesPerHour: 10 } });
  });
});

describe("admission by level", () => {
  const LEVELS: [number, number, number, number][] = [[1, 5, 15, 4], [3, 6, 20, 5], [6, 8, 25, 6], [12, 10, 30, 8], [24, 12, 40, 10], [48, 12, 60, 10]];

  it.each(LEVELS)("applies the quota of the level earned with %i credits", async (n, hourly, daily) => {
    credit(IDS[0], n);
    trust(IDS[0]);
    const limitOf = async (r: Response) => (await json(r)).error.params.limit;
    db.exec("DELETE FROM feedback_quota");
    bucket("ih", IDS[0], hourly - 1);
    expect((await send(IDS[0], `hourly-ok-${++seq}`)).status).toBe(201);
    const hourlyRefusal = await send(IDS[0], `hourly-key-${++seq}`);
    expect(hourlyRefusal.status).toBe(429);
    expect(await limitOf(hourlyRefusal)).toBe("install_hourly");
    db.exec("DELETE FROM feedback_quota");
    bucket("id", IDS[0], daily - 1);
    expect((await send(IDS[0], `daily-ok-${++seq}`)).status).toBe(201);
    expect(await limitOf(await send(IDS[0], `daily-key-${++seq}`))).toBe("install_daily");
  });

  it.each(LEVELS)("applies the reply quota of the level earned with %i credits", async (n, _h, _d, replies) => {
    credit(IDS[0], n);
    trust(IDS[0]);
    const receipt = await recorded(IDS[0], 600);
    const reply = () => call(`/v1/feedback/${receipt}/reply`, { headers: as(IDS[0]), body: { body: "fixture reply" } });
    bucket("rh", IDS[0], replies - 1);
    expect((await reply()).status).toBe(201);
    bucket("rh", IDS[0], replies);
    const refused = await reply();
    expect(refused.status).toBe(429);
    expect((await json(refused)).error.params.limit).toBe("reply_hourly");
  });

  it("admits an L0 install at 3 per hour and 10 per day", async () => {
    for (let i = 0; i < 3; i++) expect((await send(IDS[0], `l0-key-${i}`)).status).toBe(201);
    expect((await send(IDS[0], "l0-key-over")).status).toBe(429);
  });

  it("gives a lapsed install L0 ceilings and the IP limiter again, while trust-active keeps the exemption", async () => {
    credit(IDS[0], 30);
    trust(IDS[0]);
    ipAllowed = false;
    expect((await submit(IDS[0])).status).toBe(201);
    db.exec("DELETE FROM feedback_trust");
    const refused = await submit(IDS[0]);
    expect(refused.status).toBe(429);
    expect((await json(refused)).error.params.limit).toBe("ip_hourly");
    ipAllowed = true;
    expect((await submit(IDS[0])).status).toBe(201);
    bucket("ih", IDS[0], 3);
    expect((await json(await send(IDS[0], "lapsed-key-1"))).error.params.limit).toBe("install_hourly");
  });

  it("does not let a request field claim a level", async () => {
    bucket("ih", IDS[0], 3);
    db.exec("DELETE FROM feedback_quota WHERE bucket LIKE 'id:%'");
    const r = await send(IDS[0], "claim-key-1", { level: 6, adoptedCount: 99, trusted: true, installTrusted: true }, { "x-feedback-level": "6" });
    expect(r.status).toBe(429);
  });

  it("gives a blocked install byte-identical refusals to an ordinary one at every level", async () => {
    credit(IDS[0], 6);
    trust(IDS[0]);
    bucket("ih", IDS[0], 8);
    const attempt = () => send(IDS[0], "same-key-1");
    const ordinary = await attempt();
    expect(ordinary.status).toBe(429);
    block(IDS[0]);
    const blocked = await attempt();
    expect(blocked.status).toBe(ordinary.status);
    expect(await blocked.text()).toBe(await ordinary.clone().text());
    expect([...blocked.headers]).toEqual([...ordinary.headers]);
    db.exec("DELETE FROM feedback_quota");
    const concealed = await attempt();
    expect(concealed.status).toBe(429);
    expect(count("SELECT 1 FROM feedback")).toBe(0);
  });

  it("uses the held queue only for an install whose trust is live", async () => {
    credit(IDS[0], 12);
    expect(stored((await json(await submit(IDS[0]))).receipt)).toBe("held");
    trust(IDS[0]);
    expect(stored((await json(await submit(IDS[0]))).receipt)).toBe("received");
    block(IDS[0]);
    expect((await submit(IDS[0])).status).toBe(429);
  });
});

describe("revocation and block precedence", () => {
  it("does not let a later adoption undo a manual revocation", async () => {
    const first = await shipped(IDS[0], 700);
    expect(count("SELECT 1 FROM feedback_trust")).toBe(1);
    expect((await act(first, "trust", {}, "DELETE")).status).toBe(200);
    expect(count("SELECT 1 FROM feedback_trust")).toBe(0);
    await shipped(IDS[0], 701, "v1.2.3", false);
    expect(adoptions()).toBe(2);
    expect(count("SELECT 1 FROM feedback_trust")).toBe(0);
    expect((await mine(IDS[0])).profile).toMatchObject({ level: 1, trustState: "revoked", effectiveLimits: { reportsPerHour: 3, reportsPerDay: 10, repliesPerHour: 3 } });
  });

  it("restores the adoption renewal path after an explicit maintainer grant", async () => {
    const first = await shipped(IDS[0], 710);
    await act(first, "trust", {}, "DELETE");
    expect((await act(first, "trust")).status).toBe(200);
    expect((await mine(IDS[0])).profile.trustState).toBe("active");
    db.exec("DELETE FROM feedback_trust");
    await shipped(IDS[0], 711, "v1.2.3", false);
    expect(count("SELECT 1 FROM feedback_trust")).toBe(1);
  });

  it.each(["reject", "takedown"])("treats %s as a revocation that adoption cannot undo", async (action) => {
    const other = (await json(await submit(IDS[0]))).receipt as string;
    const kept = await recorded(IDS[0], 720);
    if (action === "reject") expect((await act(other, "reject", { reason: "fixture" })).status).toBe(200);
    else expect((await act(other, "takedown")).status).toBe(200);
    expect(count("SELECT 1 FROM feedback_trust")).toBe(0);
    await fix(kept);
    expect(adoptions()).toBe(1);
    expect(count("SELECT 1 FROM feedback_trust")).toBe(0);
  });

  it("does not restore revoked trust when an already credited receipt is recorded again", async () => {
    const receipt = await shipped(IDS[0], 740, "v1.2.3", false);
    await act(receipt, "trust", {}, "DELETE");
    db.exec("DELETE FROM feedback_level_state");
    expect(count("SELECT 1 FROM feedback_trust")).toBe(0);
    vi.setSystemTime(new Date(Date.parse(NOW) + 1000));
    expect(await json(await act(receipt, "adoption", { version: "v1.2.3" }))).toEqual({ receipt, adopted: true, credited: false });
    expect(count("SELECT 1 FROM feedback_trust")).toBe(0);
    expect(auditCount("adopt")).toBe(1);
  });

  it("credits an outcome for a blocked install without granting trust or lifting the block", async () => {
    const receipt = await recorded(IDS[0], 730);
    db.exec("DELETE FROM feedback_trust");
    block(IDS[0]);
    expect((await fix(receipt)).status).toBe(200);
    expect(adoptions()).toBe(1);
    expect(count("SELECT 1 FROM feedback_trust")).toBe(0);
    expect(count("SELECT 1 FROM feedback_blocks")).toBe(1);
    expect((await submit(IDS[0])).status).toBe(429);
  });
});

describe("revocation backfill in the migration", () => {
  const fresh = () => {
    const d = new DatabaseSync(":memory:");
    d.exec(migration);
    d.exec(triage);
    return d;
  };
  const H = (c: string) => c.repeat(64);
  const marked = (d: DatabaseSync) => (d.prepare("SELECT install_hash FROM feedback_level_state WHERE revoked_at IS NOT NULL ORDER BY install_hash").all() as any[]).map((r) => r.install_hash);

  it("marks installs revoked before the deploy, once, and leaves trusted and re-trusted ones alone", () => {
    const d = fresh();
    const feed = (receipt: string, hash: string, status: string) => d.prepare(
      "INSERT INTO feedback (receipt, install_hash, idempotency_key, category, body, display_name, contact, env_json, attachments_json, status, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)",
    ).run(receipt, hash, receipt, "idea", "b", "n", "", "{}", "[]", status, NOW, NOW);
    const audit = (action: string, detail: string) => d.prepare("INSERT INTO feedback_audit (at, action, detail) VALUES (?,?,?)").run(NOW, action, detail);
    feed("FB-AAAA-AAAA", H("a"), "rejected");
    feed("FB-BBBB-BBBB", H("b"), "recorded");
    audit("untrust", `FB-BBBB-BBBB install:${H("b")}`);
    feed("FB-CCCC-CCCC", H("c"), "recorded");
    audit("untrust", `FB-CCCC-CCCC install:${H("c")}`);
    audit("trust", `FB-CCCC-CCCC install:${H("c")}`);
    feed("FB-DDDD-DDDD", H("d"), "recorded");
    audit("takedown", "FB-DDDD-DDDD removed=1");
    feed("FB-EEEE-EEEE", H("e"), "rejected");
    d.prepare("INSERT INTO feedback_trust VALUES (?,?,?)").run(H("e"), NOW, "2027-01-01T00:00:00.000Z");
    feed("FB-FFFF-FFFF", H("f"), "recorded");
    d.exec(adoptionsSQL);
    expect(marked(d)).toEqual([H("a"), H("b"), H("d")]);
    d.exec("UPDATE feedback_level_state SET revoked_at = NULL");
    d.exec(adoptionsSQL);
    d.exec(adoptionsSQL);
    expect(marked(d)).toEqual([]);
    d.close();
  });
});

describe("atomicity", () => {
  it("leaves no status change, credit or trust when any statement of the transition fails", async () => {
    const receipt = await recorded(IDS[0], 800);
    db.exec("DELETE FROM feedback_trust");
    db.exec("CREATE TRIGGER fail_adopt_audit BEFORE INSERT ON feedback_audit WHEN NEW.action = 'adopt' BEGIN SELECT RAISE(ABORT, 'fixture'); END");
    await expect(fix(receipt)).rejects.toThrow();
    expect(row<any>("SELECT status FROM feedback WHERE receipt = ?", receipt).status).toBe("recorded");
    expect(count("SELECT 1 FROM feedback_adoptions")).toBe(0);
    expect(count("SELECT 1 FROM feedback_trust")).toBe(0);
    expect(count("SELECT 1 FROM feedback_level_state WHERE high_water_count > 0")).toBe(0);
    db.exec("DROP TRIGGER fail_adopt_audit");
    expect((await json(await fix(receipt))).adoption).toBe("credited");
    expect(count("SELECT 1 FROM feedback_trust")).toBe(1);
  });

  it("leaves no credit when the manual record fails midway", async () => {
    const receipt = await shipped(IDS[0], 801);
    db.exec("DELETE FROM feedback_adoptions; DELETE FROM feedback_trust");
    db.exec("CREATE TRIGGER fail_level BEFORE INSERT ON feedback_level_state BEGIN SELECT RAISE(ABORT, 'fixture'); END");
    db.exec("DELETE FROM feedback_level_state");
    await expect(act(receipt, "adoption", { version: "v1.2.3" })).rejects.toThrow();
    expect(count("SELECT 1 FROM feedback_adoptions")).toBe(0);
    expect(count("SELECT 1 FROM feedback_trust")).toBe(0);
  });

  it("records the audit row with the credit", async () => {
    await shipped(IDS[0], 802);
    const audit = row<any>("SELECT action, detail FROM feedback_audit WHERE action = 'adopt'");
    expect(audit.detail).toContain(KEY(802));
    expect(audit.detail).toContain("v1.2.3");
  });
});

describe("retention and identity", () => {
  it("keeps the ledger and level state through report and audit retention", async () => {
    await shipped(IDS[0], 900);
    vi.setSystemTime(new Date(Date.parse(NOW) + 400 * 86400000));
    await purgeStaleFeedback(env);
    expect(adoptions()).toBe(1);
    expect(count("SELECT 1 FROM feedback_level_state")).toBe(1);
  });

  it("requires the install token for an install known only by its credit", async () => {
    await submit(IDS[1]);
    credit(IDS[1], 1);
    db.exec("DELETE FROM feedback; DELETE FROM feedback_trust; DELETE FROM feedback_releases");
    tokens.set(IDS[1], "");
    const denied = await submit(IDS[1]);
    expect(denied.status).toBe(401);
    expect((await json(denied)).error.code).toBe("feedback.bad_token");
  });

  it("keeps IP binding blocks authoritative over a level", async () => {
    credit(IDS[0], 48);
    trust(IDS[0]);
    db.prepare("INSERT INTO feedback_blocks VALUES (?, 'fixture', ?, NULL)").run(`ip:${await ipHash("fixture-secret", "unknown")}`, NOW);
    const r = await submit(IDS[0]);
    expect(r.status).toBe(429);
    expect(r.headers.get("retry-after")).toBe(String(Math.ceil((Date.parse(HOUR) - Date.parse(NOW)) / 1000)));
  });
});
