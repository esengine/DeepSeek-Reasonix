// @ts-expect-error Node 22+ provides node:sqlite; Worker production code does not import it.
import { DatabaseSync } from "node:sqlite";
import { beforeEach, describe, expect, it } from "vitest";
import type { Env } from "./env";
import { d1, fakeR2 } from "./feedback_testkit";
import { purgeStaleFeedback } from "./feedback_retention";
import { handleFeedbackRoute } from "./feedback_routes";
import feedbackMigrationSQL from "../migrate-feedback.sql?raw";
import triageMigrationSQL from "../migrate-feedback-triage.sql?raw";

const ADMIN = "admin-secret";
const admin = { authorization: `Bearer ${ADMIN}` };
const PNG_B64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAAC0lEQVR4nGNgAAIAAAUAAXpeqz8AAAAASUVORK5CYII=";

let env: Env;
let raw: DatabaseSync;
let objects: Map<string, unknown>;
let ipAllowed = true;
const tokens = new Map<string, string>();
const ids = { a: "install-aaaaaaaaaaaaaaaa", b: "install-bbbbbbbbbbbbbbbb", c: "install-cccccccccccccccc" };
let seq = 0;

beforeEach(() => {
  raw = new DatabaseSync(":memory:");
  raw.exec(feedbackMigrationSQL);
  raw.exec(triageMigrationSQL);
  const r2 = fakeR2();
  objects = r2.objects;
  ipAllowed = true;
  tokens.clear();
  env = {
    DB: d1(raw),
    TELEMETRY_RAW: r2.bucket,
    FEEDBACK_LIMITER: { limit: async () => ({ success: ipAllowed }) },
    FEEDBACK_TOKEN_SECRET: "token-secret",
    FEEDBACK_ADMIN_TOKEN: ADMIN,
    FEEDBACK_ENABLED: "true",
  } as unknown as Env;
});

const call = (path: string, init: RequestInit = {}) => handleFeedbackRoute(new Request(`https://crash.test${path}`, init), env) as Promise<Response>;
const post = (path: string, body: unknown, headers: Record<string, string> = {}, method = "POST") =>
  call(path, { method, body: JSON.stringify(body), headers: { "content-type": "application/json", ...headers } });
const get = (path: string, headers: Record<string, string> = admin) => call(path, { headers });
const json = async <T = any>(r: Response) => (await r.json()) as T;
const errCode = async (r: Response) => (await json(r)).error.code as string;
const submission = (over: Record<string, unknown> = {}) => ({
  idempotencyKey: `key-${++seq}-aaaaaaaa`,
  installId: ids.a,
  category: "bug",
  body: "the composer freezes",
  displayName: "Ada",
  env: { version: "v2.24.0" },
  ...over,
});
const submit = async (over: Record<string, unknown> = {}, headers: Record<string, string> = {}) => {
  raw.exec("DELETE FROM feedback_quota WHERE bucket LIKE 'ip:%' OR bucket LIKE 'ih:%' OR bucket LIKE 'id:%'");
  const s = submission(over);
  const known = tokens.get(s.installId as string);
  const res = await post("/v1/feedback", s, known ? { "x-install-token": known, ...headers } : headers);
  if (res.status < 300) tokens.set(s.installId as string, (await json(res.clone())).installToken);
  return res;
};
const receiptOf = async (over: Record<string, unknown> = {}, headers: Record<string, string> = {}) => (await json(await submit(over, headers))).receipt as string;
const as = (id: string) => ({ "x-install-id": id, "x-install-token": tokens.get(id) ?? "" });
const act = (receipt: string, action: string, body: unknown = {}) => post(`/v1/admin/feedback/${receipt}/${action}`, body, admin);
const rowOf = (receipt: string) => raw.prepare("SELECT * FROM feedback WHERE receipt = ?").get(receipt) as any;
const statusOf = (receipt: string) => rowOf(receipt).status as string;
const mine = async (id: string) => (await json(await get("/v1/feedback/mine", as(id)))).items as any[];
const pendingItems = async () => (await json(await get("/v1/admin/feedback/pending"))).items as any[];
const hashOf = (receipt: string) => rowOf(receipt).install_hash as string;
const withImage = { attachments: [{ name: "a.png", contentType: "image/png", dataBase64: PNG_B64 }] };

describe("submit gate and trust", () => {
  it("holds a first submission and shows it to the user as received", async () => {
    const r = await receiptOf();
    expect(statusOf(r)).toBe("held");
    expect(await pendingItems()).toHaveLength(0);
    expect((await mine(ids.a))[0].status).toBe("received");
    const reply = await json(await submit());
    expect(reply.status).toBe("received");
  });

  it("auto-releases a text-only submission from an install with a released item", async () => {
    await act(await receiptOf(), "release");
    const second = await receiptOf();
    expect(statusOf(second)).toBe("received");
    expect((await pendingItems()).map((i) => i.receipt)).toContain(second);
  });

  it("does not trust an install that has only held items, or another install", async () => {
    await receiptOf();
    expect(statusOf(await receiptOf())).toBe("held");
    await act(await receiptOf(), "release");
    expect(statusOf(await receiptOf({ installId: ids.b }))).toBe("held");
  });

  it("holds a trusted submission that carries an image or is mostly links", async () => {
    await act(await receiptOf(), "release");
    expect(statusOf(await receiptOf(withImage))).toBe("held");
    const links = Array.from({ length: 5 }, (_, i) => `https://spam${i}.test`).join(" ");
    expect(statusOf(await receiptOf({ body: links }))).toBe("held");
  });

  it("backfills trust once, only for clean installs, and never re-grants after a revoke", async () => {
    const mk = (receipt: string, hash: string, status: string) =>
      raw.prepare("INSERT INTO feedback (receipt, install_hash, idempotency_key, category, body, display_name, status, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?)").run(receipt, hash, receipt, "bug", "b", "n", status, "2026-01-01T00:00:00.000Z", "2026-01-01T00:00:00.000Z");
    mk("FB-AAAA-0001", "good", "recorded");
    mk("FB-AAAA-0002", "bad", "recorded");
    mk("FB-AAAA-0003", "bad", "rejected");
    mk("FB-AAAA-0004", "taken", "recorded");
    raw.prepare("INSERT INTO feedback_audit (at, action, detail) VALUES ('t','takedown','FB-AAAA-0004 removed=1')").run();
    mk("FB-AAAA-0005", "blocked", "recorded");
    raw.prepare("INSERT INTO feedback_blocks (target, reason, created_at) VALUES ('install:blocked','x','t')").run();
    raw.exec("DELETE FROM feedback_trust");
    raw.exec("DELETE FROM feedback_config");
    raw.exec(triageMigrationSQL);
    const trusted = () => (raw.prepare("SELECT install_hash FROM feedback_trust").all() as { install_hash: string }[]).map((r) => r.install_hash);
    expect(trusted()).toEqual(["good"]);
    raw.exec("DELETE FROM feedback_trust");
    raw.exec(triageMigrationSQL);
    raw.exec(triageMigrationSQL);
    expect(trusted()).toEqual([]);
  });

  it("lets trust lapse thirty days after the last release", async () => {
    await act(await receiptOf(), "release");
    expect(statusOf(await receiptOf())).toBe("received");
    raw.prepare("UPDATE feedback_trust SET expires_at = ?").run(new Date(Date.now() - 1000).toISOString());
    expect(statusOf(await receiptOf())).toBe("held");
    const again = await receiptOf();
    await act(again, "release");
    const row = raw.prepare("SELECT expires_at FROM feedback_trust").get() as { expires_at: string };
    expect(Date.parse(row.expires_at) - Date.now()).toBeGreaterThan(29 * 86_400_000);
    expect(statusOf(await receiptOf())).toBe("received");
  });

  it("revokes trust when feedback is rejected or taken down", async () => {
    const r = await receiptOf();
    await act(r, "release");
    expect(statusOf(await receiptOf())).toBe("received");
    const held = await receiptOf(withImage);
    await act(held, "reject", { reason: "spam" });
    expect(statusOf(await receiptOf())).toBe("held");
  });
});

describe("images stay private until released", () => {
  const keyOf = () => [...objects.keys()][0].slice("feedback/".length);

  it("answers 404 before release and after a release without publishImages", async () => {
    const r = await receiptOf(withImage);
    expect((await call(`/v1/feedback/attachments/${keyOf()}`)).status).toBe(404);
    await act(r, "release");
    expect((await call(`/v1/feedback/attachments/${keyOf()}`)).status).toBe(404);
    expect((await pendingItems())[0].attachments).toEqual([]);
  });

  it("serves and lists the image only after release with publishImages", async () => {
    const r = await receiptOf(withImage);
    await act(r, "release", { publishImages: true });
    expect((await call(`/v1/feedback/attachments/${keyOf()}`)).status).toBe(200);
    const item = (await pendingItems())[0];
    expect(item.attachments).toEqual([expect.objectContaining({ contentType: "image/png", released: true })]);
    expect(item.attachments[0].url).toContain(keyOf());
  });

  it.each(["https://crash.test", "https://reasonix-crash-report.reasonix.workers.dev", "https://crash.reasonix.io"])(
    "builds attachment urls from the public origin when asked via %s",
    async (origin) => {
      const r = await receiptOf(withImage);
      await act(r, "release", { publishImages: true });
      const res = await handleFeedbackRoute(new Request(`${origin}/v1/admin/feedback/pending`, { headers: admin }), env);
      const item = (await json(res as Response)).items[0];
      expect(item.attachments[0].url).toBe(`https://crash.reasonix.io/v1/feedback/attachments/${keyOf()}`);
    },
  );

  it("lets the operator view an unreleased image behind the admin token only", async () => {
    const r = await receiptOf(withImage);
    const path = `/v1/admin/feedback/${r}/attachments/${keyOf()}`;
    expect((await get(path, {})).status).toBe(401);
    expect((await get(path)).status).toBe(200);
    expect((await get(`/v1/admin/feedback/${r}/attachments/${"z".repeat(24)}`)).status).toBe(404);
  });

  it("takedown deletes the object, unpublishes it and records an audit row", async () => {
    const r = await receiptOf(withImage);
    const key = keyOf();
    await act(r, "release", { publishImages: true });
    const res = await act(r, "takedown");
    expect(await json(res)).toMatchObject({ attachmentsRemoved: true, removed: 1 });
    expect(objects.size).toBe(0);
    expect((await call(`/v1/feedback/attachments/${key}`)).status).toBe(404);
    expect(rowOf(r).attachments_json).toBe("[]");
    expect(raw.prepare("SELECT action FROM feedback_audit").all()).toContainEqual({ action: "takedown" });
    expect((await act(r, "takedown")).status).toBe(200);
  });

  it("keeps an answered report's image for the operator, never public, and purges it with the report", async () => {
    const r = await receiptOf(withImage);
    const key = keyOf();
    await act(r, "answer", { body: "see the issue" });
    expect(objects.size).toBe(1);
    expect(JSON.parse(rowOf(r).attachments_json)).toHaveLength(1);
    expect((await get(`/v1/admin/feedback/${r}/attachments/${key}`)).status).toBe(200);
    expect((await get(`/v1/admin/feedback/${r}/attachments/${key}`, {})).status).toBe(401);
    expect((await call(`/v1/feedback/attachments/${key}`)).status).toBe(404);
    expect(raw.prepare("SELECT COUNT(*) AS n FROM feedback_public_images").get()).toEqual({ n: 0 });
    expect((await mine(ids.a)).find((i) => i.receipt === r)?.attachments ?? []).toEqual([]);
    expect(await errCode(await act(r, "release", { publishImages: true }))).toBe("feedback.bad_transition");
    expect((await json(await get(`/v1/admin/feedback/${r}`))).attachments).toHaveLength(1);
    const old = new Date(Date.now() - 40 * 86_400_000).toISOString();
    raw.prepare("UPDATE feedback SET created_at = ?, updated_at = ?").run(old, old);
    await purgeStaleFeedback(env);
    expect(objects.size).toBe(0);
  });

  it("still deletes an answered report's image through delete-screenshots", async () => {
    const r = await receiptOf(withImage);
    await act(r, "answer", { body: "x" });
    expect(await json(await act(r, "takedown"))).toMatchObject({ attachmentsRemoved: true, removed: 1 });
    expect(objects.size).toBe(0);
  });

  it("deletes the image when feedback is rejected", async () => {
    const r = await receiptOf(withImage);
    await act(r, "reject", { reason: "spam" });
    expect(objects.size).toBe(0);
  });
});

describe("contact stays out of the converter's views", () => {
  it("shows contact only in the triage listing and detail", async () => {
    const r = await receiptOf({ contact: "ada@example.test" });
    expect(JSON.stringify(await json(await get("/v1/admin/feedback/held")))).toContain("ada@example.test");
    expect(JSON.stringify(await json(await get(`/v1/admin/feedback/${r}`)))).toContain("ada@example.test");
    await act(r, "release");
    expect(await (await get("/v1/admin/feedback/pending")).text()).not.toContain("example.test");
    expect(await (await get("/v1/admin/feedback/open")).text()).not.toContain("example.test");
    expect((await pendingItems())[0]).toMatchObject({ status: "received" });
  });

  it("does not leak contact through replies/pending or the user's own feed", async () => {
    const r = await receiptOf({ contact: "ada@example.test" });
    await act(r, "release");
    await act(r, "recorded", { issueNumber: 4, issueUrl: "https://github.com/o/r/issues/4" });
    await post(`/v1/feedback/${r}/reply`, { body: "more detail" }, as(ids.a));
    expect(await (await get("/v1/admin/feedback/replies/pending")).text()).not.toContain("example.test");
  });
});

describe("blocks", () => {
  const hash = async () => hashOf(await receiptOf({ installId: ids.b }));

  it("answers a blocked install exactly like a rate limit", async () => {
    ipAllowed = false;
    const limited = await submit({ installId: ids.c });
    ipAllowed = true;
    await post("/v1/admin/feedback/block", { target: `install:${await hash()}`, reason: "spam" }, admin);
    const blocked = await submit({ installId: ids.b });
    expect(blocked.status).toBe(limited.status);
    expect(await blocked.text()).toBe(await limited.text());
    expect(blocked.status).toBe(429);
  });

  it("blocks by IP and by IPv6 /64 without storing the raw address", async () => {
    await post("/v1/admin/feedback/block", { target: "ip:203.0.113.7", reason: "abuse" }, admin);
    await post("/v1/admin/feedback/block", { target: "ip:2001:db8:1:2::", reason: "abuse" }, admin);
    expect(await errCode(await submit({}, { "cf-connecting-ip": "203.0.113.7" }))).toBe("feedback.rate_limited");
    expect(await errCode(await submit({ installId: ids.b }, { "cf-connecting-ip": "2001:db8:1:2:aaaa::9" }))).toBe("feedback.rate_limited");
    expect((await submit({ installId: ids.c }, { "cf-connecting-ip": "203.0.113.8" })).status).toBe(201);
    const listed = await (await get("/v1/admin/feedback/blocks")).text();
    expect(listed).not.toContain("203.0.113.7");
  });

  it("expires timed blocks, lifts them on DELETE, and rejects malformed targets", async () => {
    const h = await hash();
    await post("/v1/admin/feedback/block", { target: `install:${h}`, reason: "x", hours: 1 }, admin);
    expect(await errCode(await submit({ installId: ids.b }))).toBe("feedback.rate_limited");
    raw.prepare("UPDATE feedback_blocks SET expires_at = ?").run(new Date(Date.now() - 1000).toISOString());
    expect((await submit({ installId: ids.b })).status).toBe(201);
    await post("/v1/admin/feedback/block", { target: `install:${h}`, reason: "x" }, admin);
    expect(await errCode(await submit({ installId: ids.b }))).toBe("feedback.rate_limited");
    await post("/v1/admin/feedback/block", { target: `install:${h}` }, admin, "DELETE");
    expect((await submit({ installId: ids.b })).status).toBe(201);
    for (const target of ["install:xyz", "ip:not-an-ip", "ip:999.1.1.1", "user:1"]) {
      expect(await errCode(await post("/v1/admin/feedback/block", { target, reason: "x" }, admin))).toBe("feedback.invalid");
    }
  });

  it("blocks a trusted install and stops its auto-release", async () => {
    const r = await receiptOf();
    await act(r, "release");
    await post("/v1/admin/feedback/block", { target: `install:${hashOf(r)}`, reason: "x" }, admin);
    expect(await errCode(await submit())).toBe("feedback.rate_limited");
  });

  it("blocks a blocked install's replies too", async () => {
    const r = await receiptOf();
    await act(r, "ask", { body: "which os?" });
    await post("/v1/admin/feedback/block", { target: `install:${hashOf(r)}`, reason: "x" }, admin);
    expect(await errCode(await post(`/v1/feedback/${r}/reply`, { body: "win" }, as(ids.a)))).toBe("feedback.rate_limited");
  });

  it("auto-blocks an install after three rejections within seven days", async () => {
    for (let i = 0; i < 2; i++) {
      await act(await receiptOf(), "reject", { reason: "spam" });
      expect((await submit()).status).toBe(201);
      raw.exec("UPDATE feedback SET status = status");
    }
    const third = await receiptOf();
    const res = await json(await act(third, "reject", { reason: "spam" }));
    expect(res.installBlocked).toBe(true);
    expect(await errCode(await submit())).toBe("feedback.rate_limited");
    const block = raw.prepare("SELECT expires_at FROM feedback_blocks").get() as { expires_at: string };
    expect(Date.parse(block.expires_at) - Date.now()).toBeGreaterThan(6.9 * 86_400_000);
  });

  it("does not count rejections older than seven days or under three", async () => {
    const a = await receiptOf();
    const b = await receiptOf();
    await act(a, "reject", { reason: "spam" });
    await act(b, "reject", { reason: "spam" });
    raw.prepare("UPDATE feedback SET updated_at = ? WHERE receipt = ?").run(new Date(Date.now() - 8 * 86_400_000).toISOString(), a);
    const c = await receiptOf();
    expect((await json(await act(c, "reject", { reason: "spam" }))).installBlocked).toBe(false);
    expect((await submit()).status).toBe(201);
  });

  it("never shortens a longer manual block when auto-blocking", async () => {
    const r = await receiptOf();
    await post("/v1/admin/feedback/block", { target: `install:${hashOf(r)}`, reason: "manual" }, admin);
    for (let i = 0; i < 3; i++) raw.prepare("INSERT INTO feedback (receipt, install_hash, idempotency_key, category, body, display_name, status, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?)").run(`FB-AAAA-000${i}`, hashOf(r), `k${i}`, "bug", "b", "n", "held", "t", "t");
    for (let i = 0; i < 3; i++) await act(`FB-AAAA-000${i}`, "reject", { reason: "x" });
    expect(raw.prepare("SELECT expires_at, reason FROM feedback_blocks").get()).toEqual({ expires_at: null, reason: "manual" });
  });
});

describe("global cap fairness", () => {
  const day = new Date().toISOString().slice(0, 10);
  const setGlobal = (n: number) => raw.prepare("INSERT OR REPLACE INTO feedback_quota (bucket, n, day) VALUES (?, ?, ?)").run(`g:${day}`, n, day);

  it("keeps the last tenth for trusted installs", async () => {
    await act(await receiptOf(), "release");
    setGlobal(270);
    expect(await errCode(await submit({ installId: ids.b }))).toBe("feedback.busy");
    expect((await submit()).status).toBe(201);
    setGlobal(300);
    expect(await errCode(await submit())).toBe("feedback.busy");
  });

  it("lets an audited admin override the daily cap", async () => {
    await act(await receiptOf(), "release");
    expect(await errCode(await post("/v1/admin/feedback/cap", { dailyGlobal: -1 }, admin))).toBe("feedback.invalid");
    expect(await errCode(await post("/v1/admin/feedback/cap", { dailyGlobal: 99999 }, admin))).toBe("feedback.invalid");
    setGlobal(300);
    expect(await errCode(await submit())).toBe("feedback.busy");
    expect((await post("/v1/admin/feedback/cap", { dailyGlobal: 400 }, admin)).status).toBe(200);
    expect((await submit()).status).toBe(201);
    setGlobal(360);
    expect(await errCode(await submit({ installId: ids.b }))).toBe("feedback.busy");
    expect(await json(await get("/v1/admin/feedback/cap"))).toEqual({ dailyGlobal: 400, overridden: true });
    expect(raw.prepare("SELECT detail FROM feedback_audit WHERE action = 'cap'").all()).toEqual([{ detail: "dailyGlobal=400" }]);
  });
});

describe("triage transitions", () => {
  it("lists held and needs_info items oldest first with trust and install hash", async () => {
    const a = await receiptOf();
    const b = await receiptOf({ installId: ids.b });
    await act(b, "ask", { body: "which os?" });
    const { items } = await json(await get("/v1/admin/feedback/held"));
    expect(items.map((i: any) => [i.receipt, i.status])).toEqual([[a, "held"], [b, "needs_info"]]);
    expect(items[0]).toMatchObject({ installHash: hashOf(a), installTrusted: false });
    expect(items[1].replies).toEqual([expect.objectContaining({ author: "maintainer", body: "which os?" })]);
  });

  it("moves held to answered, needs_info, rejected only and never backwards", async () => {
    const a = await receiptOf();
    expect((await act(a, "answer", { body: "fixed in 2.25" })).status).toBe(200);
    expect(statusOf(a)).toBe("answered");
    expect(await errCode(await act(a, "ask", { body: "q" }))).toBe("feedback.bad_transition");
    expect(await errCode(await act(a, "release"))).toBe("feedback.bad_transition");
    expect(await errCode(await act(a, "reject", { reason: "x" }))).toBe("feedback.bad_transition");
    expect(await errCode(await post(`/v1/admin/feedback/${a}/status`, { status: "in_progress" }, admin))).toBe("feedback.bad_transition");
    expect(await errCode(await act(a, "recorded", { issueNumber: 1, issueUrl: "https://github.com/o/r/issues/1" }))).toBe("feedback.bad_transition");
    const b = await receiptOf();
    await act(b, "release");
    for (const action of ["ask", "answer"]) expect(await errCode(await act(b, action, { body: "q" }))).toBe("feedback.bad_transition");
    expect(await errCode(await act(b, "reject", { reason: "x" }))).toBe("feedback.bad_transition");
  });

  it("validates bodies and scrubs stored maintainer replies", async () => {
    const a = await receiptOf();
    expect(await errCode(await act(a, "ask", { body: "" }))).toBe("feedback.invalid");
    expect(await errCode(await act(a, "ask", { body: "x".repeat(4097) }))).toBe("feedback.invalid");
    expect(await errCode(await act(a, "reject", {}))).toBe("feedback.invalid");
    await act(a, "ask", { body: "mail me at ops@example.test" });
    expect((await mine(ids.a))[0].replies[0].body).not.toContain("ops@example.test");
    expect(await errCode(await act("FB-ZZZZ-ZZZZ", "ask", { body: "q" }))).toBe("feedback.not_found");
  });

  it("lets the maintainer reply without changing state, but not on rejected feedback", async () => {
    const a = await receiptOf();
    expect((await act(a, "reply", { body: "noted" })).status).toBe(200);
    expect(statusOf(a)).toBe("held");
    await act(a, "reject", { reason: "x" });
    expect(await errCode(await act(a, "reply", { body: "noted" }))).toBe("feedback.not_replyable");
  });

  it("guards release against concurrent change", async () => {
    const a = await receiptOf();
    const real = env.DB;
    env.DB = {
      prepare: real.prepare.bind(real),
      batch: async (list: unknown[]) => {
        raw.prepare("UPDATE feedback SET status = 'rejected' WHERE receipt = ?").run(a);
        return real.batch(list as D1PreparedStatement[]);
      },
    } as unknown as D1Database;
    expect(await errCode(await act(a, "release"))).toBe("feedback.bad_transition");
    expect(statusOf(a)).toBe("rejected");
  });

  it("requires the admin bearer token on every new endpoint", async () => {
    const r = await receiptOf();
    const paths: [string, string][] = [
      ["GET", "/v1/admin/feedback/held"],
      ["GET", "/v1/admin/feedback/list"],
      ["GET", `/v1/admin/feedback/${r}`],
      ["POST", `/v1/admin/feedback/${r}/release`],
      ["POST", `/v1/admin/feedback/${r}/reject`],
      ["POST", `/v1/admin/feedback/${r}/answer`],
      ["POST", `/v1/admin/feedback/${r}/ask`],
      ["POST", `/v1/admin/feedback/${r}/reply`],
      ["POST", `/v1/admin/feedback/${r}/takedown`],
      ["GET", "/v1/admin/feedback/replies/pending"],
      ["POST", "/v1/admin/feedback/replies/1/ack"],
      ["POST", "/v1/admin/feedback/block"],
      ["DELETE", "/v1/admin/feedback/block"],
      ["GET", "/v1/admin/feedback/blocks"],
      ["POST", "/v1/admin/feedback/cap"],
      ["GET", "/v1/admin/feedback/cap"],
    ];
    for (const [i, [method, path]] of paths.entries()) {
      const ip = { "cf-connecting-ip": `203.0.113.${i + 1}` };
      expect((await call(path, { method, headers: ip })).status, `${method} ${path}`).toBe(401);
      expect((await call(path, { method, headers: { ...ip, authorization: "Bearer wrong" } })).status, `${method} ${path}`).toBe(401);
    }
    expect(statusOf(r)).toBe("held");
  });
});

describe("user replies", () => {
  const reply = (r: string, body: unknown, id = ids.a) => post(`/v1/feedback/${r}/reply`, body, as(id));

  it("moves needs_info back to held when the user replies", async () => {
    const r = await receiptOf();
    await act(r, "ask", { body: "which os?" });
    expect((await mine(ids.a))[0]).toMatchObject({ status: "needs_info", needsInput: true });
    const res = await reply(r, { body: "windows 11" });
    expect(res.status).toBe(201);
    expect(typeof (await json(res)).replyId).toBe("number");
    expect(statusOf(r)).toBe("held");
    const item = (await mine(ids.a))[0];
    expect(item).toMatchObject({ status: "received", needsInput: false, replyCount: 2 });
    expect(item.replies.map((t: any) => [t.author, t.body])).toEqual([["maintainer", "which os?"], ["user", "windows 11"]]);
    expect((await json(await get("/v1/admin/feedback/held"))).items[0].replies).toHaveLength(2);
    expect(await json(await get("/v1/admin/feedback/replies/pending"))).toEqual({ items: [] });
  });

  it("refuses replies on held, received, rejected, closed or someone else's feedback", async () => {
    const r = await receiptOf();
    expect(await errCode(await reply(r, { body: "hi" }))).toBe("feedback.not_replyable");
    await act(r, "release");
    expect(await errCode(await reply(r, { body: "hi" }))).toBe("feedback.not_replyable");
    const rej = await receiptOf();
    await act(rej, "reject", { reason: "x" });
    expect(await errCode(await reply(rej, { body: "hi" }))).toBe("feedback.not_replyable");
    await receiptOf({ installId: ids.b });
    const q = await receiptOf();
    await act(q, "ask", { body: "?" });
    expect(await errCode(await reply(q, { body: "hi" }, ids.b))).toBe("feedback.not_replyable");
    expect(await errCode(await post(`/v1/feedback/${q}/reply`, { body: "hi" }, { "x-install-id": ids.a, "x-install-token": "nope" }))).toBe("feedback.bad_token");
    raw.prepare("UPDATE feedback SET status = 'fixed' WHERE receipt = ?").run(r);
    expect(await errCode(await reply(r, { body: "hi" }))).toBe("feedback.not_replyable");
  });

  it("caps a report at ten user replies", async () => {
    const r = await receiptOf();
    await act(r, "answer", { body: "done" });
    for (let i = 0; i < 10; i++) {
      raw.exec("DELETE FROM feedback_quota WHERE bucket LIKE 'rh:%'");
      expect((await reply(r, { body: `r${i}` })).status).toBe(201);
    }
    raw.exec("DELETE FROM feedback_quota WHERE bucket LIKE 'rh:%'");
    const over = await reply(r, { body: "eleven" });
    expect(over.status).toBe(429);
    expect(await errCode(over)).toBe("feedback.reply_limit");
    expect(raw.prepare("SELECT COUNT(*) AS n FROM feedback_replies WHERE author = 'user'").get()).toEqual({ n: 10 });
  });

  it("caps an install at three replies per hour and refunds refused attempts", async () => {
    const r = await receiptOf();
    await act(r, "answer", { body: "done" });
    for (let i = 0; i < 3; i++) expect((await reply(r, { body: `r${i}` })).status).toBe(201);
    const limited = await reply(r, { body: "fourth" });
    expect(limited.status).toBe(429);
    expect(await errCode(limited)).toBe("feedback.rate_limited");
    const hold = await receiptOf();
    expect(await errCode(await reply(hold, { body: "x" }))).toBe("feedback.rate_limited");
  });

  it("limits reply size and content, and scrubs secrets", async () => {
    const r = await receiptOf();
    await act(r, "answer", { body: "done" });
    expect(await errCode(await reply(r, { body: "x".repeat(4097) }))).toBe("feedback.invalid");
    expect(await errCode(await reply(r, { body: "   " }))).toBe("feedback.invalid");
    expect(await errCode(await reply(r, { nope: 1 }))).toBe("feedback.invalid");
    expect((await reply(r, { body: "x".repeat(4096) })).status).toBe(201);
    expect((await reply(r, { body: "key sk-abcdefghijklmnopqrstuvwx" })).status).toBe(201);
    const stored = raw.prepare("SELECT body FROM feedback_replies WHERE author = 'user'").all() as { body: string }[];
    expect(stored.some((s) => s.body.includes("sk-abcdefgh"))).toBe(false);
    const res = await call(`/v1/feedback/${r}/reply`, { method: "POST", body: "x".repeat(20_000), headers: as(ids.a) });
    expect(res.status).toBe(413);
  });

  it("honours the per-IP limiter", async () => {
    const r = await receiptOf();
    await act(r, "answer", { body: "done" });
    ipAllowed = false;
    expect(await errCode(await reply(r, { body: "hi" }))).toBe("feedback.rate_limited");
  });

  it("hands replies on converted reports to the converter once, by id", async () => {
    const r = await receiptOf();
    await act(r, "release");
    await act(r, "recorded", { issueNumber: 9, issueUrl: "https://github.com/o/r/issues/9" });
    await reply(r, { body: "still happens" });
    const { items } = await json(await get("/v1/admin/feedback/replies/pending"));
    expect(items).toEqual([{ id: expect.stringMatching(/^[A-Za-z0-9_-]{1,64}$/), receipt: r, body: "still happens", issueNumber: 9 }]);
    expect((await post(`/v1/admin/feedback/replies/${items[0].id}/ack`, {}, admin)).status).toBe(200);
    expect((await json(await get("/v1/admin/feedback/replies/pending"))).items).toEqual([]);
    expect(await errCode(await post("/v1/admin/feedback/replies/999/ack", {}, admin))).toBe("feedback.not_found");
    expect(await errCode(await post("/v1/admin/feedback/replies/abc/ack", {}, admin))).toBe("feedback.not_found");
  });

  it("lists replies to answered reports with no issue number", async () => {
    const r = await receiptOf();
    await act(r, "answer", { body: "done" });
    await reply(r, { body: "thanks" });
    expect((await json(await get("/v1/admin/feedback/replies/triage"))).items[0]).toMatchObject({ receipt: r, issueNumber: null });
    expect((await json(await get("/v1/admin/feedback/replies/pending"))).items).toEqual([]);
  });
});

describe("user-visible status names", () => {
  it("maps held to received and rejected to closed without a reason", async () => {
    const a = await receiptOf();
    const b = await receiptOf();
    await act(b, "reject", { reason: "obvious spam" });
    const items = await mine(ids.a);
    expect(items.find((i) => i.receipt === a).status).toBe("received");
    const closed = items.find((i) => i.receipt === b);
    expect(closed.status).toBe("closed");
    expect(JSON.stringify(closed)).not.toContain("spam");
  });

  it("returns at most the newest 20 replies, oldest first", async () => {
    const r = await receiptOf();
    await act(r, "answer", { body: "m0" });
    for (let i = 1; i < 25; i++) await act(r, "reply", { body: `m${i}` });
    const replies = (await mine(ids.a))[0].replies;
    expect(replies).toHaveLength(20);
    expect(replies[0].body).toBe("m5");
    expect(replies[19].body).toBe("m24");
  });
});

describe("turnstile", () => {
  it("is off by default and fails closed when configured", async () => {
    expect((await submit()).status).toBe(201);
    env.FEEDBACK_TURNSTILE_SECRET = "ts-secret";
    expect(await errCode(await submit({ installId: ids.b }))).toBe("feedback.challenge_required");
    const real = globalThis.fetch;
    const seen: string[] = [];
    try {
      globalThis.fetch = (async (_u: string, init: { body: URLSearchParams }) => {
        seen.push(init.body.get("response") ?? "");
        return new Response(JSON.stringify({ success: init.body.get("response") === "good", action: "feedback", hostname: "app.test" }));
      }) as unknown as typeof fetch;
      const bad = await submit({ installId: ids.b, turnstileToken: "bad" });
      expect(bad.status).toBe(403);
      expect((await submit({ installId: ids.b, turnstileToken: "good" })).status).toBe(201);
      globalThis.fetch = (async () => { throw new Error("down"); }) as unknown as typeof fetch;
      expect(await errCode(await submit({ installId: ids.c, turnstileToken: "good" }))).toBe("feedback.challenge_required");
    } finally {
      globalThis.fetch = real;
    }
    expect(seen).toEqual(["bad", "good"]);
  });

  it("does not spend quota on a failed challenge", async () => {
    env.FEEDBACK_TURNSTILE_SECRET = "ts-secret";
    await submit();
    expect(raw.prepare("SELECT COUNT(*) AS n FROM feedback_quota").get()).toEqual({ n: 0 });
  });
});

describe("retention and migration", () => {
  it("purges stale answered and rejected reports with replies and expired blocks", async () => {
    const a = await receiptOf();
    await act(a, "answer", { body: "done" });
    const b = await receiptOf(withImage);
    await act(b, "reject", { reason: "x" });
    const old = new Date(Date.now() - 40 * 86_400_000).toISOString();
    raw.prepare("UPDATE feedback SET created_at = ?, updated_at = ?").run(old, old);
    await post("/v1/admin/feedback/block", { target: `install:${hashOf(a)}`, reason: "x", hours: 1 }, admin);
    raw.prepare("UPDATE feedback_blocks SET expires_at = ?").run(old);
    raw.prepare("INSERT INTO feedback_audit (at, action, detail) VALUES (?, 'x', 'y')").run(new Date(Date.now() - 200 * 86_400_000).toISOString());
    await purgeStaleFeedback(env);
    for (const t of ["feedback", "feedback_replies", "feedback_blocks"]) {
      expect((raw.prepare(`SELECT COUNT(*) AS n FROM ${t}`).get() as { n: number }).n, t).toBe(0);
    }
    expect(raw.prepare("SELECT COUNT(*) AS n FROM feedback_audit WHERE action = 'x'").get()).toEqual({ n: 0 });
    expect(objects.size).toBe(0);
  });

  it("re-applies cleanly and keeps the indexes the queries rely on", () => {
    raw.exec(triageMigrationSQL);
    raw.exec(triageMigrationSQL);
    const names = (raw.prepare("SELECT name FROM sqlite_master WHERE type = 'index'").all() as { name: string }[]).map((r) => r.name);
    for (const n of ["feedback_replies_receipt", "feedback_replies_unhandled", "feedback_blocks_expires", "feedback_public_images_receipt", "feedback_audit_at", "feedback_install_status_updated"]) expect(names).toContain(n);
  });

  it("answers the hot queries from an index", () => {
    const plan = (sql: string) => (raw.prepare(`EXPLAIN QUERY PLAN ${sql}`).all() as { detail: string }[]).map((r) => r.detail).join(" ");
    expect(plan("SELECT COUNT(*) FROM feedback WHERE install_hash = 'h' AND status = 'rejected' AND updated_at >= 't'")).toContain("feedback_install_status_updated");
    expect(plan("SELECT * FROM feedback_replies WHERE receipt IN ('a') ORDER BY id DESC LIMIT 5")).toContain("feedback_replies_receipt");
    expect(plan("SELECT id FROM feedback_replies WHERE author = 'user' AND handled = 0 ORDER BY id LIMIT 5")).toContain("feedback_replies_unhandled");
    expect(plan("SELECT * FROM feedback WHERE status = 'held' ORDER BY created_at ASC LIMIT 5")).toContain("feedback_status_created");
  });
});

describe("review hardening", () => {
  const reply = (r: string, body: unknown, id = ids.a) => post(`/v1/feedback/${r}/reply`, body, as(id));
  const failBatchOnce = () => {
    const real = env.DB;
    let armed = true;
    env.DB = {
      prepare: real.prepare.bind(real),
      batch: async (list: D1PreparedStatement[]) => {
        if (armed) {
          armed = false;
          throw new Error("d1 unavailable");
        }
        return real.batch(list);
      },
    } as unknown as D1Database;
  };

  it("keeps the converter's reply queue free of issue-less replies", async () => {
    for (let i = 0; i < 6; i++) {
      const r = await receiptOf({ installId: `install-${String(i).padStart(16, "x")}` });
      await act(r, "answer", { body: "done" });
      await reply(r, { body: `thanks ${i}` }, `install-${String(i).padStart(16, "x")}`);
    }
    const c = await receiptOf({ installId: ids.c });
    await act(c, "release");
    await act(c, "recorded", { issueNumber: 5, issueUrl: "https://github.com/o/r/issues/5" });
    await reply(c, { body: "on the issue" }, ids.c);
    const pending = (await json(await get("/v1/admin/feedback/replies/pending?limit=1"))).items;
    expect(pending).toEqual([expect.objectContaining({ receipt: c, issueNumber: 5 })]);
    expect((await json(await get("/v1/admin/feedback/replies/triage?limit=50"))).items).toHaveLength(6);
  });

  it("answers a blocked install exactly like an unblocked one until it submits something new", async () => {
    const first = await submit();
    const replayKey = "replay-key-aaaaaaaa";
    const created = await submit({ idempotencyKey: replayKey });
    expect(created.status).toBe(201);
    await post("/v1/admin/feedback/block", { target: `install:${hashOf((await json(first)).receipt)}`, reason: "x" }, admin);
    const noToken = await post("/v1/feedback", submission());
    expect(noToken.status).toBe(401);
    expect(await errCode(noToken)).toBe("feedback.bad_token");
    const replay = await post("/v1/feedback", submission({ idempotencyKey: replayKey }), { "x-install-token": "anything" });
    expect(replay.status).toBe(200);
    expect(await errCode(await submit())).toBe("feedback.rate_limited");
  });

  it("serves released images with a short, non-immutable cache lifetime", async () => {
    const r = await receiptOf(withImage);
    await act(r, "release", { publishImages: true });
    const res = await call(`/v1/feedback/attachments/${[...objects.keys()][0].slice(9)}`);
    expect(res.headers.get("cache-control")).toBe("public, max-age=300");
    const admin2 = await get(`/v1/admin/feedback/${r}/attachments/${[...objects.keys()][0].slice(9)}`);
    expect(admin2.headers.get("content-disposition")).toBe("attachment");
  });

  it("rolls reject back completely when the transaction fails, and a retry completes", async () => {
    const r = await receiptOf();
    await act(await receiptOf(), "release");
    failBatchOnce();
    await expect(act(r, "reject", { reason: "spam" })).rejects.toThrow();
    expect(statusOf(r)).toBe("held");
    expect(raw.prepare("SELECT COUNT(*) AS n FROM feedback_audit WHERE action = 'reject'").get()).toEqual({ n: 0 });
    expect((await act(r, "reject", { reason: "spam" })).status).toBe(200);
    expect(statusOf(r)).toBe("rejected");
    expect(raw.prepare("SELECT COUNT(*) AS n FROM feedback_trust").get()).toEqual({ n: 0 });
  });

  it("completes the missing side effects when reject is repeated on an applied state", async () => {
    const r = await receiptOf(withImage);
    const realDelete = (env.TELEMETRY_RAW as unknown as { delete: (k: string[]) => Promise<void> }).delete;
    let fail = true;
    (env.TELEMETRY_RAW as unknown as { delete: unknown }).delete = async (k: string[]) => {
      if (fail) throw new Error("r2 down");
      return realDelete(k);
    };
    await expect(act(r, "reject", { reason: "spam" })).rejects.toThrow();
    expect(statusOf(r)).toBe("rejected");
    expect(objects.size).toBe(1);
    fail = false;
    expect((await act(r, "reject", { reason: "spam" })).status).toBe(200);
    expect(objects.size).toBe(0);
    expect(rowOf(r).attachments_json).toBe("[]");
  });

  it("commits an answer and its reply together and survives a retry", async () => {
    const r = await receiptOf({ contact: "ada@example.test" });
    failBatchOnce();
    await expect(act(r, "answer", { body: "done" })).rejects.toThrow();
    expect(statusOf(r)).toBe("held");
    expect(raw.prepare("SELECT COUNT(*) AS n FROM feedback_replies").get()).toEqual({ n: 0 });
    expect((await act(r, "answer", { body: "done" })).status).toBe(200);
    expect((await act(r, "answer", { body: "done" })).status).toBe(200);
    expect(raw.prepare("SELECT COUNT(*) AS n FROM feedback_replies").get()).toEqual({ n: 1 });
    expect(rowOf(r).contact).toBe("");
  });

  it("never stores a reply for a report that moved on concurrently", async () => {
    const r = await receiptOf();
    const real = env.DB;
    env.DB = {
      prepare: real.prepare.bind(real),
      batch: async (list: D1PreparedStatement[]) => {
        raw.prepare("UPDATE feedback SET status = 'rejected' WHERE receipt = ?").run(r);
        return real.batch(list);
      },
    } as unknown as D1Database;
    expect(await errCode(await act(r, "answer", { body: "late" }))).toBe("feedback.bad_transition");
    expect(raw.prepare("SELECT COUNT(*) AS n FROM feedback_replies").get()).toEqual({ n: 0 });
  });

  it("writes a user reply and the needs_info -> held move in one transaction", async () => {
    const r = await receiptOf();
    await act(r, "ask", { body: "which os?" });
    failBatchOnce();
    await expect(reply(r, { body: "win" })).rejects.toThrow();
    expect(statusOf(r)).toBe("needs_info");
    expect(raw.prepare("SELECT COUNT(*) AS n FROM feedback_replies WHERE author = 'user'").get()).toEqual({ n: 0 });
  });

  it("does not expose rejection through an idempotent replay", async () => {
    const s = submission();
    const created = await json(await post("/v1/feedback", s));
    await act(created.receipt, "reject", { reason: "spam" });
    const replay = await json(await post("/v1/feedback", s));
    expect(replay.status).toBe("closed");
  });

  it("counts every reply in replyCount while listing only the newest 20", async () => {
    const r = await receiptOf();
    await act(r, "answer", { body: "m0" });
    for (let i = 1; i < 25; i++) await act(r, "reply", { body: `m${i}` });
    const item = (await mine(ids.a))[0];
    expect(item.replyCount).toBe(25);
    expect(item.replies).toHaveLength(20);
  });

  it("keeps a report alive when a reply arrives inside the retention window", async () => {
    const r = await receiptOf();
    await act(r, "answer", { body: "done" });
    const old = new Date(Date.now() - 40 * 86_400_000).toISOString();
    raw.prepare("UPDATE feedback SET created_at = ?, updated_at = ? WHERE receipt = ?").run(old, old, r);
    expect((await reply(r, { body: "one more thing" })).status).toBe(201);
    await purgeStaleFeedback(env);
    expect(rowOf(r)).toBeTruthy();
    raw.prepare("UPDATE feedback SET updated_at = ? WHERE receipt = ?").run(old, r);
    await purgeStaleFeedback(env);
    expect(rowOf(r)).toBeUndefined();
  });

  it("takedown revokes trust", async () => {
    const r = await receiptOf(withImage);
    const first = await receiptOf();
    await act(first, "release");
    expect(statusOf(await receiptOf())).toBe("received");
    await act(r, "takedown");
    expect(statusOf(await receiptOf())).toBe("held");
  });

  it("lets only the owning install reply", async () => {
    const r = await receiptOf();
    await act(r, "ask", { body: "?" });
    await receiptOf({ installId: ids.b });
    expect(await errCode(await reply(r, { body: "not mine" }, ids.b))).toBe("feedback.not_replyable");
    expect(statusOf(r)).toBe("needs_info");
    expect(raw.prepare("SELECT COUNT(*) AS n FROM feedback_replies WHERE author = 'user'").get()).toEqual({ n: 0 });
  });

  it("validates the Turnstile action and hostname, bounds the call, and spends no quota on failure", async () => {
    env.FEEDBACK_TURNSTILE_SECRET = "ts-secret";
    let limiterCalls = 0;
    env.FEEDBACK_LIMITER = { limit: async () => { limiterCalls++; return { success: true }; } };
    let signal: AbortSignal | null | undefined;
    let reply: Record<string, unknown> = { success: true, action: "feedback", hostname: "app.test" };
    const real = globalThis.fetch;
    globalThis.fetch = (async (_u: string, init: RequestInit) => { signal = init.signal; return new Response(JSON.stringify(reply)); }) as unknown as typeof fetch;
    try {
      reply = { success: true, action: "other", hostname: "app.test" };
      expect(await errCode(await submit({ turnstileToken: "t" }))).toBe("feedback.challenge_required");
      env.FEEDBACK_TURNSTILE_HOSTNAMES = "app.test";
      reply = { success: true, action: "feedback", hostname: "evil.test" };
      expect(await errCode(await submit({ turnstileToken: "t" }))).toBe("feedback.challenge_required");
      expect(limiterCalls).toBe(0);
      expect(signal).toBeTruthy();
      reply = { success: true, action: "feedback", hostname: "app.test" };
      expect((await submit({ turnstileToken: "t" })).status).toBe(201);
    } finally {
      globalThis.fetch = real;
    }
  });
});
