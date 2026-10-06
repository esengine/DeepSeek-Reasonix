import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { mintFeedbackAssertion, retryFeedbackErasures } from "./feedbackLink";
import { World } from "./testing/world";

const SECRET = "account-secret";
const VECTOR = "v1.eyJhdWQiOiJmZWVkYmFjayIsImV4cCI6MTkwMDAwMDAwMCwic3ViIjoiNDIifQ.LdIhInX_RkQHYDYKzx1m0y90dGq432OfPhFsQeWUD0I";
const ERASE_VECTOR = "KoDfcAUpCN0XCPai7d56pgUHNTQcqXCe6SDxiAOKDw0";

beforeEach(() => {
  vi.stubGlobal("fetch", vi.fn(async () => Response.json({ ok: true })));
});

afterEach(() => vi.unstubAllGlobals());

function decode(assertion: string): { aud: string; exp: number; sub: string } {
  const part = (assertion.split(".")[1] ?? "").replace(/-/g, "+").replace(/_/g, "/");
  return JSON.parse(atob(part));
}

describe("feedback assertion", () => {
  it("matches the vector the crash-report worker verifies", async () => {
    const minted = await mintFeedbackAssertion("vector-secret", 42, new Date((1_900_000_000 - 300) * 1000));
    expect(minted.assertion).toBe(VECTOR);
    expect(minted.expiresAt).toBe(new Date(1_900_000_000 * 1000).toISOString());
  });

  it("is minted for the signed-in account only, with a five minute lifetime and the feedback audience", async () => {
    const world = new World({ bindings: { FEEDBACK_ACCOUNT_SECRET: SECRET } });
    const account = await world.addAccount();
    const before = Math.floor(Date.now() / 1000);
    const res = await world.call("POST", "/me/feedback-assertion", { cookie: account.cookie });
    expect(res.status).toBe(200);
    const claims = decode(res.body.assertion);
    expect(claims).toMatchObject({ aud: "feedback", sub: String(account.id) });
    expect(claims.exp - before).toBeGreaterThanOrEqual(299);
    expect(claims.exp - before).toBeLessThanOrEqual(302);
    expect(JSON.stringify(res.body)).not.toContain(SECRET);
    expect(res.body.assertion).not.toContain(account.email);
  });

  it("refuses without a session, for a signed-out session and for a deleted or suspended account", async () => {
    const world = new World({ bindings: { FEEDBACK_ACCOUNT_SECRET: SECRET } });
    expect((await world.call("POST", "/me/feedback-assertion")).status).toBe(401);
    const gone = await world.addAccount();
    world.store.raw.prepare("UPDATE users SET status = 'suspended' WHERE id = ?").run(gone.id);
    expect((await world.call("POST", "/me/feedback-assertion", { cookie: gone.cookie })).status).toBe(401);
    const deleted = await world.addAccount();
    await world.call("DELETE", "/me", { cookie: deleted.cookie });
    expect((await world.call("POST", "/me/feedback-assertion", { cookie: deleted.cookie })).status).toBe(401);
  });

  it("answers 503 feedback_unavailable while the shared secret is unset", async () => {
    const world = new World();
    const account = await world.addAccount();
    const res = await world.call("POST", "/me/feedback-assertion", { cookie: account.cookie });
    expect(res.status).toBe(503);
    expect(res.body.error.code).toBe("feedback_unavailable");
  });

  it("works for a device-flow session as well as a browser one", async () => {
    const world = new World({ bindings: { FEEDBACK_ACCOUNT_SECRET: SECRET } });
    const account = await world.addAccount();
    const cli = await world.addSession(account.id, "cli");
    expect((await world.call("POST", "/me/feedback-assertion", { cookie: cli.cookie })).status).toBe(200);
  });
});

describe("erasure call on account deletion", () => {
  const ok = () => vi.fn(async () => Response.json({ ok: true }));
  const pending = (world: World) => world.rows<{ user_id: number; attempts: number }>("SELECT user_id, attempts FROM feedback_erasures");

  it("sends one signed erase request and keeps the row until a delivery after the assertion window", async () => {
    const fetchMock = ok();
    vi.stubGlobal("fetch", fetchMock);
    const world = new World({ bindings: { FEEDBACK_ACCOUNT_SECRET: "vector-secret" } });
    const account = await world.addAccount();
    const res = await world.call("DELETE", "/me", { cookie: account.cookie });
    expect(res).toEqual({ status: 200, body: { ok: true } });
    const calls = fetchMock.mock.calls as unknown as [string, RequestInit][];
    const erase = calls.filter(([url]) => String(url).endsWith("/v1/feedback/account/erase"));
    expect(erase).toHaveLength(1);
    const [url, init] = erase[0]!;
    expect(url).toBe("https://crash.reasonix.io/v1/feedback/account/erase");
    expect(init.method).toBe("POST");
    expect(init.body).toBe(JSON.stringify({ sub: String(account.id) }));
    expect(String((init.headers as Record<string, string>)["x-erase-signature"])).toMatch(/^[A-Za-z0-9_-]{43}$/);
    expect(pending(world)).toEqual([{ user_id: account.id, attempts: 1 }]);
    expect(await retryFeedbackErasures(world.env, new Date(Date.now() + 60_000))).toBe(0);
    expect(pending(world)).toEqual([{ user_id: account.id, attempts: 2 }]);
    expect(await retryFeedbackErasures(world.env, new Date(Date.now() + 10 * 60_000))).toBe(1);
    expect(pending(world)).toEqual([]);
  });

  it("sends a second erase after the window so a link made with a pre-deletion assertion is cleared", async () => {
    const links = new Set<string>();
    const worker = vi.fn(async (_url: string, init?: RequestInit) => {
      const body = String(init?.body ?? "");
      if (body.startsWith("{")) for (const key of [...links]) if (key.startsWith(`${(JSON.parse(body) as { sub: string }).sub}:`)) links.delete(key);
      return Response.json({ ok: true });
    });
    vi.stubGlobal("fetch", worker);
    const world = new World({ bindings: { FEEDBACK_ACCOUNT_SECRET: SECRET } });
    const account = await world.addAccount();
    const minted = await world.call("POST", "/me/feedback-assertion", { cookie: account.cookie });
    expect(minted.status).toBe(200);
    await world.call("DELETE", "/me", { cookie: account.cookie });
    links.add(`${account.id}:install-1`);
    expect(links.size).toBe(1);
    expect(await retryFeedbackErasures(world.env, new Date(Date.now() + 10 * 60_000))).toBe(1);
    expect(links.size).toBe(0);
    expect(worker).toHaveBeenCalledTimes(2);
  });

  it("limits assertion minting per account and keeps other accounts unaffected", async () => {
    const world = new World({ bindings: { FEEDBACK_ACCOUNT_SECRET: SECRET } });
    const a = await world.addAccount();
    const b = await world.addAccount();
    let limited = 0;
    for (let i = 0; i < 40; i++) if ((await world.call("POST", "/me/feedback-assertion", { cookie: a.cookie })).status === 429) limited++;
    expect(limited).toBeGreaterThan(0);
    expect((await world.call("POST", "/me/feedback-assertion", { cookie: b.cookie })).status).toBe(200);
  });

  it("retries the least recently attempted rows first so failing ones cannot starve the rest", async () => {
    const world = new World({ bindings: { FEEDBACK_ACCOUNT_SECRET: SECRET } });
    for (let id = 1; id <= 60; id++) {
      world.store.raw.prepare("INSERT INTO feedback_erasures (user_id, created_at, attempts, last_attempt_at) VALUES (?, '2026-10-01T00:00:00.000Z', ?, ?)")
        .run(id, id <= 50 ? 5 : 0, id <= 50 ? "2026-10-05T00:00:00.000Z" : null);
    }
    const seen: string[] = [];
    vi.stubGlobal("fetch", vi.fn(async (_u: string, init?: RequestInit) => {
      seen.push((JSON.parse(String(init?.body)) as { sub: string }).sub);
      return new Response(null, { status: 500 });
    }));
    await retryFeedbackErasures(world.env, new Date("2026-10-06T00:00:00.000Z"));
    expect(seen.slice(0, 10).sort()).toEqual(["51", "52", "53", "54", "55", "56", "57", "58", "59", "60"].sort());
  });

  it("warns with counts only when the backlog is old or keeps failing", async () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    vi.stubGlobal("fetch", vi.fn(async () => new Response(null, { status: 401 })));
    const world = new World({ bindings: { FEEDBACK_ACCOUNT_SECRET: SECRET } });
    world.store.raw.prepare("INSERT INTO feedback_erasures (user_id, created_at, attempts) VALUES (777, '2026-10-01T00:00:00.000Z', 0)").run();
    await retryFeedbackErasures(world.env, new Date("2026-10-02T00:00:00.000Z"));
    expect(warn).not.toHaveBeenCalled();
    await retryFeedbackErasures(world.env, new Date("2026-10-06T00:00:00.000Z"));
    const printed = JSON.stringify(warn.mock.calls);
    expect(warn).toHaveBeenCalledTimes(1);
    expect(printed).toMatch(/1 pending/);
    expect(printed).not.toContain("777");
    warn.mockRestore();
  });

  it("signs exactly the body the worker verifies", async () => {
    const fetchMock = ok();
    vi.stubGlobal("fetch", fetchMock);
    const world = new World({ bindings: { FEEDBACK_ACCOUNT_SECRET: "vector-secret" } });
    world.store.raw.prepare("INSERT INTO feedback_erasures (user_id, created_at, attempts) VALUES (42, '2026-10-01T00:00:00.000Z', 0)").run();
    await retryFeedbackErasures(world.env, new Date("2026-10-06T00:00:00.000Z"));
    const [, init] = (fetchMock.mock.calls as unknown as [string, RequestInit][])[0]!;
    expect(init.body).toBe('{"sub":"42"}');
    expect((init.headers as Record<string, string>)["x-erase-signature"]).toBe(ERASE_VECTOR);
  });

  it.each([
    ["a 503", () => vi.fn(async () => new Response(null, { status: 503 }))],
    ["a 401", () => vi.fn(async () => new Response(null, { status: 401 }))],
    ["a network error", () => vi.fn(async () => { throw new Error("offline"); })],
  ])("still deletes the account when the worker answers %s, and keeps the request for a retry", async (_n, make) => {
    vi.stubGlobal("fetch", make());
    const world = new World({ bindings: { FEEDBACK_ACCOUNT_SECRET: SECRET } });
    const account = await world.addAccount();
    const res = await world.call("DELETE", "/me", { cookie: account.cookie });
    expect(res).toEqual({ status: 200, body: { ok: true } });
    expect(world.rows("SELECT status FROM users WHERE id = ?", account.id)[0]).toEqual({ status: "deleted" });
    expect(pending(world)).toEqual([{ user_id: account.id, attempts: 1 }]);
  });

  it("retries from the scheduled job until it lands, then stops", async () => {
    const fetchMock = vi.fn(async () => new Response(null, { status: 503 }));
    vi.stubGlobal("fetch", fetchMock);
    const world = new World({ bindings: { FEEDBACK_ACCOUNT_SECRET: SECRET } });
    const account = await world.addAccount();
    await world.call("DELETE", "/me", { cookie: account.cookie });
    const later = new Date(Date.now() + 10 * 60_000);
    expect(await retryFeedbackErasures(world.env, later)).toBe(0);
    expect(pending(world)).toEqual([{ user_id: account.id, attempts: 2 }]);
    vi.stubGlobal("fetch", ok());
    expect(await retryFeedbackErasures(world.env, later)).toBe(1);
    expect(pending(world)).toEqual([]);
    expect(await retryFeedbackErasures(world.env, later)).toBe(0);
  });

  it("does not call out and keeps the request while the secret is unset", async () => {
    const fetchMock = ok();
    vi.stubGlobal("fetch", fetchMock);
    const world = new World();
    const account = await world.addAccount();
    expect((await world.call("DELETE", "/me", { cookie: account.cookie })).status).toBe(200);
    expect(fetchMock).not.toHaveBeenCalled();
    expect(pending(world)).toHaveLength(1);
    expect(await retryFeedbackErasures(world.env, new Date())).toBe(0);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("does not fail the deletion when the outbox itself cannot be written", async () => {
    vi.stubGlobal("fetch", ok());
    const world = new World({ bindings: { FEEDBACK_ACCOUNT_SECRET: SECRET }, fail: (sql) => sql.includes("feedback_erasures") });
    const account = await world.addAccount();
    const res = await world.call("DELETE", "/me", { cookie: account.cookie });
    expect(res).toEqual({ status: 200, body: { ok: true } });
    expect(world.rows("SELECT status FROM users WHERE id = ?", account.id)[0]).toEqual({ status: "deleted" });
  });

  it("honours an origin override and bounds each retry batch", async () => {
    const fetchMock = ok();
    vi.stubGlobal("fetch", fetchMock);
    const world = new World({ bindings: { FEEDBACK_ACCOUNT_SECRET: SECRET, FEEDBACK_ORIGIN: "https://crash.example/" } });
    for (let id = 1; id <= 60; id++) {
      world.store.raw.prepare("INSERT INTO feedback_erasures (user_id, created_at, attempts) VALUES (?, '2026-10-01T00:00:00.000Z', 0)").run(id);
    }
    expect(await retryFeedbackErasures(world.env, new Date("2026-10-06T00:00:00.000Z"))).toBe(50);
    expect(String((fetchMock.mock.calls as unknown as [string][])[0]![0])).toBe("https://crash.example/v1/feedback/account/erase");
    expect(pending(world)).toHaveLength(10);
  });

  it("never logs the secret, the signature or the account email", async () => {
    const spies = (["log", "info", "warn", "error"] as const).map((k) => vi.spyOn(console, k).mockImplementation(() => {}));
    vi.stubGlobal("fetch", vi.fn(async () => new Response(null, { status: 500 })));
    const world = new World({ bindings: { FEEDBACK_ACCOUNT_SECRET: SECRET } });
    const account = await world.addAccount();
    await world.call("DELETE", "/me", { cookie: account.cookie });
    await retryFeedbackErasures(world.env, new Date());
    const printed = JSON.stringify(spies.flatMap((s) => s.mock.calls));
    expect(printed).not.toContain(SECRET);
    expect(printed).not.toContain(account.email);
    spies.forEach((s) => s.mockRestore());
  });

  it("keeps the migration additive: every earlier table and index is unchanged", () => {
    const world = new World();
    const all = () => world.rows<{ name: string; sql: string }>("SELECT name, sql FROM sqlite_master WHERE name NOT LIKE 'sqlite_%' ORDER BY name");
    const withTable = all();
    expect(withTable.some((r) => r.name === "feedback_erasures")).toBe(true);
    const bare = new World({ bindings: {} });
    bare.store.raw.exec("DROP TABLE feedback_erasures");
    expect(withTable.filter((r) => r.name !== "feedback_erasures")).toEqual(
      bare.rows<{ name: string; sql: string }>("SELECT name, sql FROM sqlite_master WHERE name NOT LIKE 'sqlite_%' ORDER BY name"),
    );
  });
});
