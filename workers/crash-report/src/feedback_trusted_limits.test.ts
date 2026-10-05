// @ts-expect-error Node 22+ provides node:sqlite for boundary tests.
import { DatabaseSync } from "node:sqlite";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Env } from "./env";
import { d1 } from "./feedback_testkit";
import { handleFeedbackRoute } from "./feedback_routes";
import { purgeStaleFeedback } from "./feedback_retention";
import migration from "../migrate-feedback.sql?raw";
import triage from "../migrate-feedback-triage.sql?raw";
import adoptionsMigrationSQL from "../migrate-feedback-adoptions.sql?raw";

const NOW = "2026-10-03T12:34:56.000Z";
const HOUR = "2026-10-03T13:00:00.000Z";
const DAY = "2026-10-04T00:00:00.000Z";
const ID = "install-aaaaaaaaaaaaaaaa";
const admin = { authorization: "Bearer fixture-admin" };
let db: DatabaseSync;
let env: Env;
let token: string;
let hash: string;
let receipt: string;
let seq = 0;
let ipAllowed: boolean;
const call = async (path: string, body?: unknown, headers: Record<string, string> = admin, method = body === undefined ? "GET" : "POST") =>
  await handleFeedbackRoute(new Request(`https://crash.test${path}`, { method, headers, ...(body === undefined ? {} : { body: JSON.stringify(body) }) }), env) as Response;
const submit = (extra = {}, key = `fixture-key-${++seq}`) => call("/v1/feedback", {
  installId: ID, idempotencyKey: key, body: "Neutral fixture", category: "bug", displayName: "Fixture", env: {}, ...extra,
}, { "x-install-token": token });
const act = (action: string, body = {}, r = receipt) => call(`/v1/admin/feedback/${r}/${action}`, body);
const trust = (days = 30) => db.prepare("INSERT OR REPLACE INTO feedback_trust VALUES (?,?,?)").run(hash, NOW, new Date(Date.parse(NOW) + days * 86400000).toISOString());
const quota = (prefix: string, n: number) => db.prepare("INSERT OR REPLACE INTO feedback_quota VALUES (?,?,?)").run(`${prefix}:${hash}:${prefix === "id" ? NOW.slice(0, 10) : NOW.slice(0, 13)}`, n, NOW.slice(0, 10));
const block = (expiry: string | null) => db.prepare("INSERT OR REPLACE INTO feedback_blocks VALUES (?,?,?,?)").run(`install:${hash}`, "fixture", NOW, expiry);
const params = async (r: Response) => (await r.json() as any).error.params;
const expectWindow = async (r: Response, limit: string, resetsAt: string | null) => {
  expect(r.status).toBe(429);
  const seconds = resetsAt === null ? null : Math.ceil((Date.parse(resetsAt) - Date.parse(NOW)) / 1000);
  expect(await params(r)).toEqual({ limit, resetsAt, retryAfterSeconds: seconds });
  expect(r.headers.get("retry-after")).toBe(seconds === null ? null : String(seconds));
};

beforeEach(async () => {
  vi.useFakeTimers();
  vi.setSystemTime(NOW);
  db = new DatabaseSync(":memory:");
  db.exec(migration);
  db.exec(triage);
  db.exec(adoptionsMigrationSQL);
  ipAllowed = true;
  env = { DB: d1(db), FEEDBACK_ENABLED: "true", FEEDBACK_TOKEN_SECRET: "fixture-secret", FEEDBACK_ADMIN_TOKEN: "fixture-admin",
    FEEDBACK_LIMITER: { limit: async () => ({ success: ipAllowed }) } } as unknown as Env;
  token = "";
  const first = await submit();
  const j = await first.json() as any;
  token = j.installToken;
  receipt = j.receipt;
  hash = (db.prepare("SELECT install_hash FROM feedback").get() as any).install_hash;
  db.exec("DELETE FROM feedback_quota");
});
afterEach(() => { vi.unstubAllGlobals(); vi.useRealTimers(); db.close(); });

describe("retained identity ownership", () => {
  it.each(["established", "manual"])("requires proof after %s trust outlives reports", async (tier) => {
    if (tier === "manual") expect((await act("trust")).status).toBe(200);
    else {
      for (let i = 0; i < 5; i++) {
        db.exec("DELETE FROM feedback_quota");
        const r = i === 0 ? receipt : (await (await submit()).json() as any).receipt;
        expect((await act("release", {}, r)).status).toBe(200);
      }
    }
    vi.setSystemTime(new Date(Date.parse(NOW) + 32 * 86400000));
    await purgeStaleFeedback(env);
    expect(db.prepare("SELECT * FROM feedback").get()).toBeUndefined();
    ipAllowed = false;
    const ownedToken = token;
    for (const presented of ["", "wrong-fixture-token"]) {
      token = presented;
      const denied = await submit();
      expect(denied.status).toBe(401);
      expect((await denied.json() as any).error.code).toBe("feedback.bad_token");
      expect(db.prepare("SELECT * FROM feedback").get()).toBeUndefined();
      expect(db.prepare("SELECT * FROM feedback_quota").get()).toBeUndefined();
    }
    token = ownedToken;
    const key = `retained-${tier}-replay`;
    const accepted = await submit({}, key);
    expect(accepted.status).toBe(201);
    const original = await accepted.json() as any;
    expect(original.status).toBe("received");
    expect(original.installToken).toBe(ownedToken);
    block(null);
    expect((await submit()).status).toBe(429);
    const { ipHash } = await import("./feedback_crypto");
    db.exec("DELETE FROM feedback_blocks");
    db.prepare("INSERT INTO feedback_blocks VALUES (?, 'fixture', ?, NULL)")
      .run(`ip:${await ipHash("fixture-secret", "unknown")}`, NOW);
    expect((await submit()).status).toBe(429);
    token = "";
    env.FEEDBACK_TURNSTILE_SECRET = "fixture-challenge";
    const replay = await submit({}, key);
    expect(replay.status).toBe(200);
    expect(await replay.json()).toEqual(original);
    const stranger = await submit({ installId: "install-bbbbbbbbbbbbbbbb" });
    expect(stranger.status).toBe(403);
    delete env.FEEDBACK_TURNSTILE_SECRET;
    expect((await submit({ installId: "install-bbbbbbbbbbbbbbbb" })).status).toBe(429);
  });

  it("keeps release identity proof after trust revocation and report retention", async () => {
    await act("release");
    await call(`/v1/admin/feedback/${receipt}/trust`, undefined, admin, "DELETE");
    vi.setSystemTime(new Date(Date.parse(NOW) + 32 * 86400000));
    await purgeStaleFeedback(env);
    const ownedToken = token;
    token = "";
    expect((await submit()).status).toBe(401);
    token = ownedToken;
    const accepted = await submit();
    expect(accepted.status).toBe(201);
    expect((db.prepare("SELECT status FROM feedback").get() as any).status).toBe("held");
  });
});

describe("enabled challenge concealment", () => {
  it.each(["missing", "rejected", "accepted"])("uses the same %s challenge gate with and without blocks", async (challenge) => {
    env.FEEDBACK_TURNSTILE_SECRET = "fixture-challenge";
    quota("ih", 3);
    const fetcher = vi.fn(async () => new Response(JSON.stringify({ success: challenge === "accepted", action: "feedback" })));
    vi.stubGlobal("fetch", fetcher);
    const extra = challenge === "missing" ? {} : { turnstileToken: "fixture-response" };
    const ordinary = await submit(extra);
    expect(ordinary.status).toBe(challenge === "accepted" ? 429 : 403);
    for (const target of [`install:${hash}`, `ip:${await (await import("./feedback_crypto")).ipHash("fixture-secret", "unknown")}`]) {
      db.exec("DELETE FROM feedback_blocks");
      db.prepare("INSERT INTO feedback_blocks VALUES (?, 'fixture', ?, NULL)").run(target, NOW);
      const blocked = await submit(extra);
      expect(blocked.status).toBe(ordinary.status);
      expect(await blocked.text()).toBe(await ordinary.clone().text());
      expect([...blocked.headers]).toEqual([...ordinary.headers]);
    }
    expect(fetcher).toHaveBeenCalledTimes(challenge === "missing" ? 0 : 3);
    expect(db.prepare("SELECT * FROM feedback_quota WHERE bucket LIKE 'g:%'").get()).toBeUndefined();
    expect((db.prepare("SELECT COUNT(*) AS n FROM feedback").get() as any).n).toBe(1);
  });

  it("admits valid challenges and preserves replay recovery without another verification", async () => {
    env.FEEDBACK_TURNSTILE_SECRET = "fixture-challenge";
    const fetcher = vi.fn(async () => new Response(JSON.stringify({ success: true, action: "feedback" })));
    vi.stubGlobal("fetch", fetcher);
    const key = "challenge-fixture-replay";
    const accepted = await submit({ turnstileToken: "fixture-response" }, key);
    expect(accepted.status).toBe(201);
    const original = await accepted.json();
    block(null);
    token = "";
    const replay = await submit({}, key);
    expect(replay.status).toBe(200);
    expect(await replay.json()).toEqual(original);
    expect(fetcher).toHaveBeenCalledTimes(1);
  });
});

describe("trusted admission and concealment", () => {
  it.each([false, true])("preserves byte-identical global refusals including blocked callers (trusted=%s)", async (trusted) => {
    if (trusted) trust();
    for (const ceiling of ["burst", "daily", "zero"]) {
      db.exec("DELETE FROM feedback_blocks; DELETE FROM feedback_quota");
      delete env.FEEDBACK_BUDGET_LIMITER;
      if (ceiling === "burst") env.FEEDBACK_BUDGET_LIMITER = { limit: async () => ({ success: false }) } as any;
      else if (ceiling === "daily") db.prepare("INSERT INTO feedback_quota VALUES ('g:2026-10-03',?, '2026-10-03')").run(trusted ? 300 : 270);
      else await call("/v1/admin/feedback/cap", { dailyGlobal: 0 });
      const ordinary = await submit();
      expect(ordinary.status).toBe(503);
      for (const target of [`install:${hash}`, `ip:${await (await import("./feedback_crypto")).ipHash("fixture-secret", "unknown")}`]) {
        db.exec("DELETE FROM feedback_blocks");
        db.prepare("INSERT INTO feedback_blocks VALUES (?, 'fixture', ?, NULL)").run(target, NOW);
        const blocked = await submit();
        expect(blocked.status).toBe(ordinary.status);
        expect(await blocked.text()).toBe(await ordinary.clone().text());
        expect([...blocked.headers]).toEqual([...ordinary.headers]);
        expect(blocked.headers.get("retry-after")).toBe(ordinary.headers.get("retry-after"));
      }
      expect((db.prepare("SELECT COUNT(*) AS n FROM feedback").get() as any).n).toBe(1);
      expect(db.prepare("SELECT n FROM feedback_quota WHERE bucket LIKE 'ih:%' AND n > 0").get()).toBeUndefined();
    }
  });

  it("refunds simultaneous authenticated replay and leaves ordinary replay free", async () => {
    const base = env.DB;
    let arrivals = 0;
    let release!: () => void;
    const barrier = new Promise<void>((resolve) => { release = resolve; });
    env.DB = { ...base, prepare: (sql: string) => {
      const statement = base.prepare(sql);
      if (!sql.startsWith("SELECT * FROM feedback WHERE install_hash")) return statement;
      return { ...statement, bind: (...values: unknown[]) => {
        const bound = statement.bind(...values);
        return { ...bound, first: async () => {
          const row = await bound.first();
          if (++arrivals <= 2) {
            if (arrivals === 2) release();
            await barrier;
          }
          return row;
        } };
      } } as D1PreparedStatement;
    } } as D1Database;
    const key = "parallel-fixture-replay";
    const responses = await Promise.all([submit({}, key), submit({}, key)]);
    expect(responses.map((r) => r.status).sort()).toEqual([200, 201]);
    expect(await responses[0].json()).toEqual(await responses[1].json());
    expect((db.prepare("SELECT COUNT(*) AS n FROM feedback WHERE idempotency_key = ?").get(key) as any).n).toBe(1);
    expect(db.prepare("SELECT n FROM feedback_quota WHERE bucket NOT LIKE 'alert:%'").all()).toEqual([{ n: 1 }, { n: 1 }, { n: 1 }, { n: 1 }]);
    expect((await submit({}, key)).status).toBe(200);
    quota("ih", 3);
    expect((await submit()).status).toBe(429);
    expect((db.prepare("SELECT n FROM feedback_quota WHERE bucket LIKE 'g:%'").get() as any).n).toBe(1);
  });

  it("refunds admission when durable report insertion fails", async () => {
    db.exec("CREATE TRIGGER fail_report BEFORE INSERT ON feedback BEGIN SELECT RAISE(ABORT, 'fixture'); END");
    await expect(submit()).rejects.toThrow();
    expect(db.prepare("SELECT n FROM feedback_quota WHERE n > 0").get()).toBeUndefined();
    db.exec("DROP TRIGGER fail_report");
    expect((await submit()).status).toBe(201);
  });

  it("preserves IP, burst, caller and daily refusal precedence in combined states", async () => {
    quota("ih", 3);
    db.prepare("INSERT INTO feedback_quota VALUES ('g:2026-10-03',300,'2026-10-03')").run();
    env.FEEDBACK_BUDGET_LIMITER = { limit: async () => ({ success: false }) } as any;
    for (const blocked of [false, true]) {
      if (blocked) block(null);
      ipAllowed = false;
      await expectWindow(await submit(), "ip_hourly", HOUR);
      ipAllowed = true;
      const burst = await submit();
      expect(burst.status).toBe(503);
      expect((await params(burst)).limit).toBe("global_burst");
      delete env.FEEDBACK_BUDGET_LIMITER;
      await expectWindow(await submit(), "install_hourly", HOUR);
      env.FEEDBACK_BUDGET_LIMITER = { limit: async () => ({ success: false }) } as any;
    }
  });

  it.each([[false, 3], [true, 12]])("enforces the server tier hourly limit (trusted=%s)", async (trusted, limit) => {
    if (trusted) trust();
    for (let i = 0; i < limit; i++) expect((await submit()).status).toBe(201);
    await expectWindow(await submit(), "install_hourly", HOUR);
    expect((db.prepare("SELECT n FROM feedback_quota WHERE bucket LIKE 'g:%'").get() as any).n).toBe(limit);
    vi.setSystemTime(HOUR);
    expect((await submit()).status).toBe(201);
  });

  it.each([[false, 10], [true, 60]])("enforces daily limits and refunds hourly quota (trusted=%s)", async (trusted, limit) => {
    if (trusted) trust();
    quota("id", limit);
    await expectWindow(await submit(), "install_daily", DAY);
    expect((db.prepare("SELECT n FROM feedback_quota WHERE bucket LIKE 'ih:%'").get() as any).n).toBe(0);
    vi.setSystemTime(DAY);
    expect((await submit()).status).toBe(201);
  });

  it("exempts trusted installs from both IP ceilings, but not IP blocks", async () => {
    trust();
    ipAllowed = false;
    const { ipHash } = await import("./feedback_crypto");
    const ip = await ipHash("fixture-secret", "unknown");
    db.prepare("INSERT INTO feedback_quota VALUES (?,10,'2026-10-03')").run(`ip:${ip}:2026-10-03T12`);
    expect((await submit()).status).toBe(201);
    expect((db.prepare("SELECT n FROM feedback_quota WHERE bucket LIKE 'ip:%'").get() as any).n).toBe(10);
    expect((await call("/v1/admin/feedback/block", { target: "ip:unknown", reason: "fixture" })).status).toBe(400);
    db.prepare("INSERT INTO feedback_blocks VALUES (?, 'fixture', ?, NULL)").run(`ip:${await ipHash("fixture-secret", "unknown")}`, NOW);
    await expectWindow(await submit(), "install_hourly", HOUR);
  });

  it("does not accept client trust claims or expired trust", async () => {
    quota("ih", 3);
    trust(-1);
    await expectWindow(await submit({ trusted: true, installTrusted: true }), "install_hourly", HOUR);
    const held = await (await call("/v1/admin/feedback/held")).json() as any;
    expect(held.items[0].installTrusted).toBe(false);
  });

  it.each([false, true])("preserves byte-identical bodies and headers for blocked and ordinary callers (trusted=%s)", async (trusted) => {
    if (trusted) trust();
    quota("ih", trusted ? 12 : 3);
    const ordinary = await submit();
    await expectWindow(ordinary.clone(), "install_hourly", HOUR);
    for (const expiry of [null, "2026-10-20T09:15:00.000Z"]) {
      block(expiry);
      const blocked = await submit();
      expect(blocked.status).toBe(ordinary.status);
      expect(await blocked.text()).toBe(await ordinary.clone().text());
      expect([...blocked.headers]).toEqual([...ordinary.headers]);
    }
    db.exec("DELETE FROM feedback_quota");
    quota("id", trusted ? 60 : 10);
    const blockedDay = await submit();
    db.exec("DELETE FROM feedback_blocks");
    const ordinaryDay = await submit();
    await expectWindow(ordinaryDay.clone(), "install_daily", DAY);
    expect(await blockedDay.text()).toBe(await ordinaryDay.text());
    expect([...blockedDay.headers]).toEqual([...ordinaryDay.headers]);
  });

  it("conceals block expiry on the IP binding refusal too", async () => {
    ipAllowed = false;
    const ordinary = await submit();
    ipAllowed = true;
    block("2026-10-09T11:11:11.000Z");
    const blocked = await submit();
    expect(await blocked.text()).toBe(await ordinary.clone().text());
    expect([...blocked.headers]).toEqual([...ordinary.headers]);
    await expectWindow(ordinary, "ip_hourly", HOUR);
  });

  it("uses ordinary IP-binding precedence when a blocked caller also exhausts install quota", async () => {
    quota("ih", 3);
    ipAllowed = false;
    const ordinary = await submit();
    block(null);
    const blocked = await submit();
    expect(await blocked.text()).toBe(await ordinary.text());
    expect([...blocked.headers]).toEqual([...ordinary.headers]);
  });

  it("reports a conservative global burst wait independent of binding reset knowledge", async () => {
    env.FEEDBACK_BUDGET_LIMITER = { limit: async () => ({ success: false }) } as any;
    const r = await submit();
    expect(r.status).toBe(503);
    expect(await params(r)).toEqual({ limit: "global_burst", resetsAt: "2026-10-03T12:35:56.000Z", retryAfterSeconds: 60 });
  });

  it("preserves global reserve, trusted hard cap, zero caps, refunds and replay", async () => {
    db.prepare("INSERT INTO feedback_quota VALUES ('g:2026-10-03',270,'2026-10-03')").run();
    expect((await submit()).status).toBe(503);
    expect((db.prepare("SELECT n FROM feedback_quota WHERE bucket LIKE 'ih:%'").get() as any).n).toBe(0);
    trust();
    const key = "fixture-replay-key";
    expect((await submit({}, key)).status).toBe(201);
    db.exec("UPDATE feedback_quota SET n = 300 WHERE bucket LIKE 'g:%'");
    expect((await submit()).status).toBe(503);
    block(null);
    expect((await submit({}, key)).status).toBe(200);
    db.exec("DELETE FROM feedback_blocks; DELETE FROM feedback_quota");
    await call("/v1/admin/feedback/cap", { dailyGlobal: 0 });
    expect((await submit()).status).toBe(503);
  });
});

describe("release ledger and administration", () => {
  it("gives admin lockouts the same typed 429 shape", async () => {
    for (let i = 0; i < 5; i++) expect((await call("/v1/admin/feedback/held", undefined, { authorization: "Bearer wrong-fixture" })).status).toBe(401);
    await expectWindow(await call("/v1/admin/feedback/held"), "admin_attempts", "2026-10-03T12:45:00.000Z");
  });
  it("counts only distinct explicit releases, extends at five, survives retention and migration retries", async () => {
    for (let i = 0; i < 5; i++) {
      db.exec("DELETE FROM feedback_quota");
      const r = i === 0 ? receipt : (await (await submit()).json() as any).receipt;
      expect((await act("release", {}, r)).status).toBe(200);
      expect((await act("release", {}, r)).status).toBe(200);
      const expiry = (db.prepare("SELECT expires_at FROM feedback_trust").get() as any).expires_at;
      expect(expiry).toBe(new Date(Date.parse(NOW) + (i < 4 ? 30 : 90) * 86400000).toISOString());
    }
    db.exec("UPDATE feedback SET created_at = '2026-01-01', updated_at = '2026-01-01'");
    await purgeStaleFeedback(env);
    db.exec(triage);
    db.exec(triage);
    expect((db.prepare("SELECT COUNT(*) AS n FROM feedback_releases").get() as any).n).toBe(5);
    expect((db.prepare("SELECT COUNT(*) AS n FROM feedback").get() as any).n).toBe(0);
    const r = (await (await submit()).json() as any).receipt;
    await act("release", {}, r);
    expect((db.prepare("SELECT COUNT(*) AS n FROM feedback_releases").get() as any).n).toBe(6);
  });

  it("does not count automatic received submissions or shorten a longer grant", async () => {
    trust(365);
    const r = (await (await submit()).json() as any).receipt;
    expect((db.prepare("SELECT COUNT(*) AS n FROM feedback_releases").get() as any).n).toBe(0);
    await act("release", {}, r);
    expect((db.prepare("SELECT expires_at FROM feedback_trust").get() as any).expires_at).toBe("2027-10-03T12:34:56.000Z");
  });

  it("authenticates receipt grants/revokes, audits them, and never unblocks", async () => {
    const path = `/v1/admin/feedback/${receipt}/trust`;
    expect((await call(path, {}, {})).status).toBe(401);
    expect((await call(path, {}, admin, "PUT")).status).toBe(405);
    expect((await call(path, {})).status).toBe(200);
    expect((db.prepare("SELECT expires_at FROM feedback_trust").get() as any).expires_at).toBe("2027-10-03T12:34:56.000Z");
    await act("release");
    expect((db.prepare("SELECT expires_at FROM feedback_trust").get() as any).expires_at).toBe("2027-10-03T12:34:56.000Z");
    block(null);
    await expectWindow(await submit(), "install_hourly", HOUR);
    expect((await call(path, undefined, admin, "DELETE")).status).toBe(200);
    expect(db.prepare("SELECT * FROM feedback_trust").get()).toBeUndefined();
    expect(db.prepare("SELECT action FROM feedback_audit").all()).toEqual([{ action: "trust" }, { action: "untrust" }]);
    expect(db.prepare("SELECT * FROM feedback_blocks").get()).toBeDefined();
  });

  it.each(["POST", "DELETE"])("rolls back a %s trust mutation when audit fails", async (method) => {
    if (method === "DELETE") trust();
    db.exec("CREATE TRIGGER fail_audit BEFORE INSERT ON feedback_audit BEGIN SELECT RAISE(ABORT, 'fixture'); END");
    await expect(call(`/v1/admin/feedback/${receipt}/trust`, method === "POST" ? {} : undefined, admin, method)).rejects.toThrow();
    expect(db.prepare("SELECT * FROM feedback_trust").get() !== undefined).toBe(method === "DELETE");
  });

  it("reject and takedown revoke manual trust and auto-block still overrides it", async () => {
    expect((await act("trust")).status).toBe(200);
    await act("takedown");
    expect(db.prepare("SELECT * FROM feedback_trust").get()).toBeUndefined();
    expect((await act("trust")).status).toBe(200);
    await act("reject", { reason: "fixture" });
    expect(db.prepare("SELECT * FROM feedback_trust").get()).toBeUndefined();
    for (let i = 0; i < 2; i++) {
      db.exec("DELETE FROM feedback_quota");
      const r = (await (await submit()).json() as any).receipt;
      await act("reject", { reason: "fixture" }, r);
    }
    expect((await act("trust")).status).toBe(200);
    await expectWindow(await submit(), "install_hourly", HOUR);
  });
});

describe("reply neighbourhood", () => {
  it("refunds reply admission when the storage transaction fails and allows retry", async () => {
    trust();
    await act("ask", { body: "Neutral question" });
    const headers = { "x-install-id": ID, "x-install-token": token };
    const reply = () => call(`/v1/feedback/${receipt}/reply`, { body: "Neutral reply" }, headers);
    db.exec("CREATE TRIGGER fail_reply BEFORE INSERT ON feedback_replies BEGIN SELECT RAISE(ABORT, 'fixture'); END");
    await expect(reply()).rejects.toThrow();
    expect(db.prepare("SELECT n FROM feedback_quota WHERE n > 0 AND bucket LIKE 'rh:%'").get()).toBeUndefined();
    expect((db.prepare("SELECT status FROM feedback WHERE receipt = ?").get(receipt) as any).status).toBe("needs_info");
    db.exec("DROP TRIGGER fail_reply");
    expect((await reply()).status).toBe(201);
    expect((db.prepare("SELECT status FROM feedback WHERE receipt = ?").get(receipt) as any).status).toBe("held");
  });

  it.each([false, true])("preserves byte-identical item-cap refusals after hourly reset (trusted=%s)", async (trusted) => {
    if (trusted) trust();
    await act("answer", { body: "Neutral answer" });
    const headers = { "x-install-id": ID, "x-install-token": token };
    const reply = () => call(`/v1/feedback/${receipt}/reply`, { body: "Neutral reply" }, headers);
    for (let i = 0; i < 10; i++) {
      vi.setSystemTime(new Date(Date.parse(NOW) + i * 3600000));
      expect((await reply()).status).toBe(201);
    }
    vi.setSystemTime(new Date(Date.parse(NOW) + 10 * 3600000));
    const ordinary = await reply();
    await expectWindow(ordinary.clone(), "reply_item", null);
    for (const target of [`install:${hash}`, `ip:${await (await import("./feedback_crypto")).ipHash("fixture-secret", "unknown")}`]) {
      db.exec("DELETE FROM feedback_blocks");
      db.prepare("INSERT INTO feedback_blocks VALUES (?, 'fixture', ?, NULL)").run(target, NOW);
      const blocked = await reply();
      expect(blocked.status).toBe(ordinary.status);
      expect(await blocked.text()).toBe(await ordinary.clone().text());
      expect([...blocked.headers]).toEqual([...ordinary.headers]);
      expect(blocked.headers.get("retry-after")).toBeNull();
    }
    expect((db.prepare("SELECT COUNT(*) AS n FROM feedback_replies WHERE author = 'user'").get() as any).n).toBe(10);
  });

  it("conceals fresh untrusted reply blocks behind the ordinary IP window", async () => {
    await act("answer", { body: "Neutral answer" });
    const headers = { "x-install-id": ID, "x-install-token": token };
    ipAllowed = false;
    const ordinary = await call(`/v1/feedback/${receipt}/reply`, { body: "Neutral reply" }, headers);
    await expectWindow(ordinary.clone(), "ip_hourly", HOUR);
    ipAllowed = true;
    block(null);
    const blocked = await call(`/v1/feedback/${receipt}/reply`, { body: "Neutral reply" }, headers);
    expect(await blocked.text()).toBe(await ordinary.text());
    expect([...blocked.headers]).toEqual([...ordinary.headers]);
  });
  it.each([[false, 3], [true, 10]])("enforces reply tier with typed resets (trusted=%s)", async (trusted, limit) => {
    if (trusted) trust();
    await act("answer", { body: "Neutral answer" });
    const headers = { "x-install-id": ID, "x-install-token": token };
    if (trusted) ipAllowed = false;
    for (let i = 0; i < limit; i++) expect((await call(`/v1/feedback/${receipt}/reply`, { body: "Neutral reply" }, headers)).status).toBe(201);
    const ordinary = await call(`/v1/feedback/${receipt}/reply`, { body: "Neutral reply" }, headers);
    block("2026-11-01T00:00:00.000Z");
    const blocked = await call(`/v1/feedback/${receipt}/reply`, { body: "Neutral reply" }, headers);
    expect(await blocked.text()).toBe(await ordinary.clone().text());
    expect([...blocked.headers]).toEqual([...ordinary.headers]);
    await expectWindow(ordinary, "reply_hourly", HOUR);
    db.exec("DELETE FROM feedback_blocks");
    vi.setSystemTime(HOUR);
    const next = await call(`/v1/feedback/${receipt}/reply`, { body: "Neutral reply" }, headers);
    if (trusted) await expectWindow(next, "reply_item", null);
    else expect(next.status).toBe(201);
  });

  it("refunds invalid ownership/status and preserves ask-to-held transition", async () => {
    trust();
    const headers = { "x-install-id": ID, "x-install-token": token };
    const reply = () => call(`/v1/feedback/${receipt}/reply`, { body: "Neutral reply" }, headers);
    expect((await reply()).status).toBe(409);
    expect((db.prepare("SELECT n FROM feedback_quota WHERE bucket LIKE 'rh:%'").get() as any).n).toBe(0);
    await act("ask", { body: "Neutral question" });
    expect((await reply()).status).toBe(201);
    expect((db.prepare("SELECT status FROM feedback WHERE receipt = ?").get(receipt) as any).status).toBe("held");
    expect((await reply()).status).toBe(409);
    expect((db.prepare("SELECT n FROM feedback_quota WHERE bucket LIKE 'rh:%'").get() as any).n).toBe(1);
  });
});
