// @ts-expect-error Node 22+ provides node:sqlite; Worker production code does not import it.
import { DatabaseSync } from "node:sqlite";
import { beforeEach, describe, expect, it } from "vitest";
import type { Env } from "./env";
import { d1, fakeR2 } from "./feedback_testkit";
import { purgeStaleFeedback } from "./feedback_retention";
import { handleFeedbackRoute } from "./feedback_routes";
import feedbackMigrationSQL from "../migrate-feedback.sql?raw";
import triageMigrationSQL from "../migrate-feedback-triage.sql?raw";
import adoptionsMigrationSQL from "../migrate-feedback-adoptions.sql?raw";

const ADMIN = "admin-secret";

let env: Env;
let objects: Map<string, unknown>;
let ipAllowed = true;
let ipCalls = 0;

beforeEach(() => {
  const db = new DatabaseSync(":memory:");
  db.exec(feedbackMigrationSQL);
  db.exec(triageMigrationSQL);
  db.exec(adoptionsMigrationSQL);
  const r2 = fakeR2();
  objects = r2.objects;
  ipAllowed = true;
  ipCalls = 0;
  tokens.clear();
  env = {
    DB: d1(db),
    TELEMETRY_RAW: r2.bucket,
    FEEDBACK_LIMITER: { limit: async () => { ipCalls++; return { success: ipAllowed }; } },
    FEEDBACK_TOKEN_SECRET: "token-secret",
    FEEDBACK_ADMIN_TOKEN: ADMIN,
    FEEDBACK_ENABLED: "true",
  } as unknown as Env;
});

const call = (path: string, init: RequestInit = {}) => handleFeedbackRoute(new Request(`https://crash.test${path}`, init), env) as Promise<Response>;
const tokens = new Map<string, string>();
const submit = async (body: { installId?: string } & Record<string, unknown>, headers: Record<string, string> = {}) => {
  const id = body.installId ?? "";
  const known = tokens.get(id);
  const res = await post("/v1/feedback", body, known ? { "x-install-token": known, ...headers } : headers);
  if (res.status < 300) tokens.set(id, ((await res.clone().json()) as { installToken: string }).installToken);
  return res;
};
const post = (path: string, body: unknown, headers: Record<string, string> = {}) =>
  call(path, { method: "POST", body: JSON.stringify(body), headers: { "content-type": "application/json", ...headers } });
const admin = { authorization: `Bearer ${ADMIN}` };

const chunk = (type: string, data: number[] = []) => [0, 0, 0, data.length, ...[...type].map((c) => c.charCodeAt(0)), ...data, 0, 0, 0, 0];
const ihdr = (w = 1, h = 1) => chunk("IHDR", [w >>> 24, (w >> 16) & 255, (w >> 8) & 255, w & 255, h >>> 24, (h >> 16) & 255, (h >> 8) & 255, h & 255, 8, 2, 0, 0, 0]);
const pngBytes = (...extra: number[][]) => [0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, ...ihdr(), ...extra.flat(), ...chunk("IDAT", [1, 2, 3]), ...chunk("IEND")];
const b64 = (bytes: number[]) => btoa(String.fromCharCode(...bytes));
const PNG_B64 = b64(pngBytes());
const sof = (w = 1, h = 1) => [0xff, 0xc0, 0, 10, 8, h >> 8, h & 255, w >> 8, w & 255, 1, 0, 0];
const jpegBytes = (...segments: number[][]) => [0xff, 0xd8, ...segments.flat(), ...sof(), 0xff, 0xda, 0, 4, 0, 0, 1, 2, 0xff, 0x00, 3, 0xff, 0xd9];
const segment = (marker: number) => [0xff, marker, 0, 4, 0, 0];
let seq = 0;
const submission = (over: Record<string, unknown> = {}) => ({
  idempotencyKey: `key-${++seq}-aaaaaaaa`,
  installId: "install-aaaaaaaaaaaaaaaa",
  category: "bug",
  body: "the composer freezes",
  displayName: "Ada",
  env: { version: "v2.24.0", surface: "studio" },
  ...over,
});
const errCode = async (r: Response) => ((await r.json()) as { error: { code: string } }).error.code;

describe("POST /v1/feedback", () => {
  it("accepts a submission and returns a receipt and install token", async () => {
    const res = await submit(submission());
    expect(res.status).toBe(201);
    const j = (await res.json()) as { receipt: string; status: string; installToken: string };
    expect(j.receipt).toMatch(/^FB-[0-9A-HJKMNP-TV-Z]{4}-[0-9A-HJKMNP-TV-Z]{4}$/);
    expect(j.status).toBe("received");
    expect(j.installToken.length).toBeGreaterThan(20);
  });

  it("replays an idempotency key without creating a second row", async () => {
    const s = submission();
    const a = (await (await submit(s)).json()) as { receipt: string };
    const res = await submit(s);
    expect(res.status).toBe(200);
    expect(((await res.json()) as { receipt: string }).receipt).toBe(a.receipt);
  });

  it("caps an install at three submissions per hour", async () => {
    for (let i = 0; i < 3; i++) expect((await submit(submission())).status).toBe(201);
    const res = await submit(submission());
    expect(res.status).toBe(429);
    expect(await errCode(res)).toBe("feedback.rate_limited");
  });

  it("honours the per-IP limiter", async () => {
    ipAllowed = false;
    expect(await errCode(await submit(submission()))).toBe("feedback.rate_limited");
  });

  it("rejects an oversize body and an oversize attachment", async () => {
    const big = await submit(submission({ body: "x".repeat(8193) }));
    expect(big.status).toBe(413);
    expect(await errCode(big)).toBe("feedback.too_large");
    const png = btoa(String.fromCharCode(0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a) + "a".repeat(2 * 1024 * 1024));
    const res = await submit(submission({ attachments: [{ name: "a.png", contentType: "image/png", dataBase64: png }] }));
    expect(await errCode(res)).toBe("feedback.too_large");
  });

  it("rejects an attachment whose bytes do not match its type", async () => {
    const res = await submit(submission({ attachments: [{ name: "a.png", contentType: "image/png", dataBase64: btoa("not an image") }] }));
    expect(res.status).toBe(400);
    expect(await errCode(res)).toBe("feedback.invalid");
    expect(objects.size).toBe(0);
  });

  it("stores a valid image and serves it with nosniff", async () => {
    const res = await submit(submission({ attachments: [{ name: "shot.png", contentType: "image/png", dataBase64: PNG_B64 }] }));
    expect(res.status).toBe(201);
    const [key] = [...objects.keys()];
    expect(key.startsWith("feedback/")).toBe(true);
    await post(`/v1/admin/feedback/${((await res.json()) as { receipt: string }).receipt}/release`, { publishImages: true }, admin);
    const served = await call(`/v1/feedback/attachments/${key.slice("feedback/".length)}`);
    expect(served.status).toBe(200);
    expect(served.headers.get("content-type")).toBe("image/png");
    expect(served.headers.get("x-content-type-options")).toBe("nosniff");
    expect(served.headers.get("content-disposition")).toBe("inline");
    expect((await call("/v1/feedback/attachments/zzzzzzzzzzzzzzzzzzzz")).status).toBe(404);
  });

  it("scrubs secrets from the body server-side", async () => {
    const r = ((await (await submit(submission({ body: "my key is sk-abcdefghijklmnopqrstuvwx ok" }))).json()) as { receipt: string }).receipt;
    await post(`/v1/admin/feedback/${r}/release`, {}, admin);
    const list = (await (await call("/v1/admin/feedback/pending", { headers: admin })).json()) as { items: { body: string }[] };
    expect(list.items[0].body).not.toContain("sk-abcdefgh");
  });

  it("stays disabled while either secret is absent", async () => {
    env.FEEDBACK_ADMIN_TOKEN = undefined;
    expect(await errCode(await submit(submission()))).toBe("feedback.disabled");
    env.FEEDBACK_ADMIN_TOKEN = ADMIN;
    env.FEEDBACK_TOKEN_SECRET = undefined;
    expect(await errCode(await call("/v1/feedback/mine"))).toBe("feedback.disabled");
  });

  it("answers feedback.disabled when the kill switch is off", async () => {
    env.FEEDBACK_ENABLED = "false";
    const res = await submit(submission());
    expect(res.status).toBe(503);
    expect(await errCode(res)).toBe("feedback.disabled");
  });
});

describe("install identity", () => {
  it("issues a token once and demands it afterwards", async () => {
    const first = await post("/v1/feedback", submission());
    expect(first.status).toBe(201);
    const denied = await post("/v1/feedback", submission());
    expect(denied.status).toBe(401);
    expect(await errCode(denied)).toBe("feedback.bad_token");
    const wrong = await post("/v1/feedback", submission(), { "x-install-token": "nope" });
    expect(await errCode(wrong)).toBe("feedback.bad_token");
  });

  it("lets a replay recover the token when the first response was lost", async () => {
    const s = submission();
    const a = (await (await post("/v1/feedback", s)).json()) as { installToken: string };
    const again = await post("/v1/feedback", s);
    expect(again.status).toBe(200);
    expect(((await again.json()) as { installToken: string }).installToken).toBe(a.installToken);
  });

  it("does not store the raw install id or a plain sha256 of it", async () => {
    await submit(submission());
    const rows = await env.DB.prepare("SELECT install_hash FROM feedback").all<{ install_hash: string }>();
    const plain = Array.from(new Uint8Array(await crypto.subtle.digest("SHA-256", new TextEncoder().encode("install-aaaaaaaaaaaaaaaa"))))
      .map((b) => b.toString(16).padStart(2, "0"))
      .join("");
    expect(rows.results[0].install_hash).not.toBe(plain);
    expect(rows.results[0].install_hash).not.toContain("install-");
  });

  it("invalidates tokens when the secret rotates", async () => {
    const a = (await (await post("/v1/feedback", submission())).json()) as { installToken: string };
    env.FEEDBACK_TOKEN_SECRET = "rotated";
    const res = await call("/v1/feedback/mine", { headers: { "x-install-id": "install-aaaaaaaaaaaaaaaa", "x-install-token": a.installToken } });
    expect(await errCode(res)).toBe("feedback.bad_token");
  });
});

describe("abuse ceilings", () => {
  it("enforces the global daily cap and a per-IP hourly cap across fresh installs", async () => {
    const fresh = (n: number) => submit(submission({ installId: `install-${String(n).padStart(16, "0")}` }), { "cf-connecting-ip": "203.0.113.9" });
    for (let i = 0; i < 10; i++) expect((await fresh(i)).status).toBe(201);
    expect(await errCode(await fresh(99))).toBe("feedback.rate_limited");
    const other = await submit(submission({ installId: "install-zzzzzzzzzzzzzzzz" }), { "cf-connecting-ip": "198.51.100.1" });
    expect(other.status).toBe(201);
  });

  it("stops at the global daily cap whatever the source", async () => {
    await env.DB.prepare("INSERT INTO feedback_quota (bucket, n, day) VALUES (?, 300, ?)").bind(`g:${new Date().toISOString().slice(0, 10)}`, "x").run();
    expect(await errCode(await submit(submission()))).toBe("feedback.busy");
  });

  it("lets one IP move the global counter only by its own admitted requests", async () => {
    for (let i = 0; i < 40; i++) await submit(submission({ installId: `install-${String(i).padStart(16, "0")}` }), { "cf-connecting-ip": "203.0.113.9" });
    const g = await env.DB.prepare("SELECT n FROM feedback_quota WHERE bucket LIKE 'g:%'").first<{ n: number }>();
    expect(g?.n).toBe(10);
    const other = await submit(submission({ installId: "install-otherotherother1" }), { "cf-connecting-ip": "198.51.100.2" });
    expect(other.status).toBe(201);
  });

  it("refunds already-taken quota when a request is refused", async () => {
    const ip = { "cf-connecting-ip": "203.0.113.50" };
    const id = "install-refundrefundrefu";
    const s = await submit(submission({ installId: id }), ip);
    expect(s.status).toBe(201);
    await env.DB.prepare("UPDATE feedback_quota SET n = 10 WHERE bucket LIKE 'id:%'").run();
    for (let i = 0; i < 5; i++) expect(await errCode(await submit(submission({ installId: id }), ip))).toBe("feedback.rate_limited");
    const ipRow = await env.DB.prepare("SELECT n FROM feedback_quota WHERE bucket LIKE 'ip:%'").first<{ n: number }>();
    expect(ipRow?.n).toBe(1);
    await env.DB.prepare("UPDATE feedback_quota SET n = 300 WHERE bucket LIKE 'g:%'").run();
    await submit(submission({ installId: "install-otheroneotherone" }), ip);
    const after = await env.DB.prepare("SELECT n FROM feedback_quota WHERE bucket LIKE 'ip:%'").first<{ n: number }>();
    expect(after?.n).toBe(1);
  });

  it("counts an IPv6 client by its /64", async () => {
    for (let i = 0; i < 10; i++) await submit(submission({ installId: `install-${String(i).padStart(16, "0")}` }), { "cf-connecting-ip": `2001:db8:1:2:${i}::${i + 1}` });
    const res = await submit(submission({ installId: "install-abababababababab" }), { "cf-connecting-ip": "2001:db8:1:2:ffff::1" });
    expect(await errCode(res)).toBe("feedback.rate_limited");
  });

  it("alerts once per day when the global cap is exhausted", async () => {
    const calls: string[] = [];
    const real = globalThis.fetch;
    globalThis.fetch = (async (u: string) => void calls.push(u)) as unknown as typeof fetch;
    try {
      env.ALERT_WEBHOOK = "https://hooks.example.test/x";
      await env.DB.prepare("INSERT INTO feedback_quota (bucket, n, day) VALUES (?, 300, ?)").bind(`g:${new Date().toISOString().slice(0, 10)}`, "x").run();
      await submit(submission({ installId: "install-1111111111111111" }));
      await submit(submission({ installId: "install-2222222222222222" }));
      expect(calls).toHaveLength(1);
    } finally {
      globalThis.fetch = real;
    }
  });

  it("does not spend quota on idempotent replays", async () => {
    const s = submission();
    await submit(s);
    for (let i = 0; i < 6; i++) expect((await submit(s)).status).toBe(200);
    expect(ipCalls).toBe(1);
  });

  it("aborts a chunked body past the cap", async () => {
    const big = new ReadableStream({ start: (c) => { for (let i = 0; i < 9; i++) c.enqueue(new Uint8Array(1024 * 1024)); c.close(); } });
    const res = await call("/v1/feedback", { method: "POST", body: big, duplex: "half" } as RequestInit);
    expect(res.status).toBe(413);
  });
});

describe("attachment hygiene", () => {
  const send = (bytes: number[], contentType = "image/png") => submit(submission({ attachments: [{ name: "../../evil<script>.png", contentType, dataBase64: b64(bytes) }] }));

  it("rejects anything outside the allow-list with feedback.image_metadata", async () => {
    for (const type of ["eXIf", "tEXt", "iTXt", "zTXt", "tIME", "acTL", "prVt"]) expect(await errCode(await send(pngBytes(chunk(type, [1, 2]))))).toBe("feedback.image_metadata");
    for (const marker of [0xe1, 0xed, 0xfe, 0xe3]) expect(await errCode(await send(jpegBytes(segment(marker)), "image/jpeg"))).toBe("feedback.image_metadata");
    expect(await errCode(await send([...pngBytes(), 1, 2, 3]))).toBe("feedback.image_metadata");
    expect(await errCode(await send([...jpegBytes(), 9, 9], "image/jpeg"))).toBe("feedback.image_metadata");
    expect(objects.size).toBe(0);
  });

  it("rejects truncated images and pixel bombs without storing them", async () => {
    expect(await errCode(await send(pngBytes().slice(0, -8)))).toBe("feedback.invalid");
    expect(await errCode(await send(pngBytes().slice(0, 30)))).toBe("feedback.invalid");
    expect(await errCode(await send(jpegBytes().slice(0, -2), "image/jpeg"))).toBe("feedback.invalid");
    const bomb = [0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, ...ihdr(60000, 60000), ...chunk("IDAT", [1]), ...chunk("IEND")];
    expect(await errCode(await send(bomb))).toBe("feedback.too_large");
    expect(objects.size).toBe(0);
  });

  it("deletes stored images when a concurrent replay wins the insert", async () => {
    const real = env.DB;
    const s = submission({ attachments: [{ name: "a.png", contentType: "image/png", dataBase64: PNG_B64 }] });
    env.DB = {
      batch: real.batch,
      prepare: (sql: string) => {
        const st = real.prepare(sql);
        if (!sql.startsWith("INSERT INTO feedback (")) return st;
        return {
          bind: (...a: unknown[]) => ({
            run: async () => {
              await real.prepare("INSERT INTO feedback (receipt, install_hash, idempotency_key, category, body, display_name, status, created_at, updated_at) VALUES ('FB-RACE-RACE',?,?,'bug','b','n','received','t','t')").bind(a[1], a[2]).run();
              throw new Error("unique");
            },
          }),
        };
      },
    } as unknown as D1Database;
    const res = await submit(s);
    expect(res.status).toBe(200);
    expect(objects.size).toBe(0);
    const spent = await real.prepare("SELECT n FROM feedback_quota WHERE n > 0").first();
    expect(spent).toBeNull();
  });

  it("accepts clean images and rejects a type mismatch", async () => {
    expect((await send(jpegBytes(segment(0xe0)), "image/jpeg")).status).toBe(201);
    expect((await send(pngBytes(), "image/jpeg")).status).toBe(400);
  });

  it("names files server-side and never keeps the client's name", async () => {
    await send(pngBytes());
    const row = await env.DB.prepare("SELECT attachments_json FROM feedback").first<{ attachments_json: string }>();
    expect(row?.attachments_json).toContain("screenshot-1.png");
    expect(row?.attachments_json).not.toContain("evil");
  });

  it("serves a locked-down response", async () => {
    const r = ((await (await send(pngBytes())).json()) as { receipt: string }).receipt;
    await post(`/v1/admin/feedback/${r}/release`, { publishImages: true }, admin);
    const key = [...objects.keys()][0].slice("feedback/".length);
    const res = await call(`/v1/feedback/attachments/${key}`);
    expect(res.headers.get("content-security-policy")).toBe("default-src 'none'; sandbox");
  });
});

describe("retention", () => {
  it("drops stale unconverted feedback with its images and keeps recorded rows", async () => {
    await submit(submission({ attachments: [{ name: "a.png", contentType: "image/png", dataBase64: PNG_B64 }] }));
    const old = new Date(Date.now() - 40 * 86_400_000).toISOString();
    await env.DB.prepare("UPDATE feedback SET created_at = ?, updated_at = ?").bind(old, old).run();
    await env.DB.prepare("INSERT INTO feedback (receipt, install_hash, idempotency_key, category, body, display_name, status, created_at, updated_at) VALUES ('FB-KEEP-KEEP','h','k','bug','b','n','recorded',?,?)").bind(old, old).run();
    await purgeStaleFeedback(env);
    const left = await env.DB.prepare("SELECT receipt FROM feedback").all<{ receipt: string }>();
    expect(left.results.map((r) => r.receipt)).toEqual(["FB-KEEP-KEEP"]);
    expect(objects.size).toBe(0);
  });
});

describe("GET /v1/feedback/mine", () => {
  it("rejects a wrong token", async () => {
    const res = await call("/v1/feedback/mine", { headers: { "x-install-id": "install-aaaaaaaaaaaaaaaa", "x-install-token": "nope" } });
    expect(res.status).toBe(401);
    expect(await errCode(res)).toBe("feedback.bad_token");
  });

  it("lists only the caller's own items", async () => {
    const a = (await (await submit(submission())).json()) as { installToken: string };
    const b = (await (await submit(submission({ installId: "install-bbbbbbbbbbbbbbbb", body: "other" }))).json()) as { installToken: string };
    const mine = async (id: string, token: string) =>
      ((await (await call("/v1/feedback/mine", { headers: { "x-install-id": id, "x-install-token": token } })).json()) as { items: { titleSnippet: string }[] }).items;
    expect((await mine("install-aaaaaaaaaaaaaaaa", a.installToken)).map((i) => i.titleSnippet)).toEqual(["the composer freezes"]);
    expect((await mine("install-bbbbbbbbbbbbbbbb", b.installToken)).map((i) => i.titleSnippet)).toEqual(["other"]);
  });
});

describe("admin flow", () => {
  const receiptOf = async (s = submission()) => ((await (await submit(s)).json()) as { receipt: string }).receipt;
  const status = (r: string, body: unknown) => post(`/v1/admin/feedback/${r}/status`, body, admin);

  it("requires the admin bearer token", async () => {
    expect((await call("/v1/admin/feedback/pending")).status).toBe(401);
    expect((await call("/v1/admin/feedback/pending", { headers: { authorization: "Bearer wrong" } })).status).toBe(401);
  });

  it("walks received -> recorded -> in_progress -> fixed and never contacts leak", async () => {
    const r = await receiptOf(submission({ contact: "me@example.test" }));
    await post(`/v1/admin/feedback/${r}/release`, {}, admin);
    const pend = await call("/v1/admin/feedback/pending?limit=5", { headers: admin });
    const text = await pend.text();
    expect(text).toContain(r);
    expect(text).not.toContain("example.test");
    expect((await post(`/v1/admin/feedback/${r}/recorded`, { issueNumber: 7, issueUrl: "https://github.com/o/r/issues/7" }, admin)).status).toBe(200);
    expect((await post(`/v1/admin/feedback/${r}/recorded`, { issueNumber: 7, issueUrl: "https://github.com/o/r/issues/7" }, admin)).status).toBe(200);
    expect(((await (await call("/v1/admin/feedback/pending", { headers: admin })).json()) as { items: unknown[] }).items).toHaveLength(0);
    expect((await status(r, { status: "in_progress" })).status).toBe(200);
    const open = (await (await call("/v1/admin/feedback/open", { headers: admin })).json()) as { items: { receipt: string; issueNumber: number }[] };
    expect(open.items).toEqual([expect.objectContaining({ receipt: r, issueNumber: 7 })]);
    expect((await status(r, { status: "fixed" })).status).toBe(400);
    expect((await status(r, { status: "fixed", resolvedVersion: "next" })).status).toBe(200);
    const row = await env.DB.prepare("SELECT contact FROM feedback WHERE receipt = ?").bind(r).first<{ contact: string }>();
    expect(row?.contact).toBe("");
  });

  it("upgrades fixed(next) to a concrete version once and lists it while awaiting a tag", async () => {
    const r = await receiptOf();
    await post(`/v1/admin/feedback/${r}/release`, {}, admin);
    await post(`/v1/admin/feedback/${r}/recorded`, { issueNumber: 3, issueUrl: "https://github.com/o/r/issues/3" }, admin);
    await status(r, { status: "fixed", resolvedVersion: "next" });
    const open = async () => ((await (await call("/v1/admin/feedback/open", { headers: admin })).json()) as { items: { receipt: string }[] }).items;
    expect(await open()).toEqual([expect.objectContaining({ receipt: r })]);
    expect(await errCode(await status(r, { status: "fixed", resolvedVersion: "latest" }))).toBe("feedback.bad_transition");
    expect((await status(r, { status: "fixed", resolvedVersion: "v2.25.0" })).status).toBe(200);
    expect(await open()).toHaveLength(0);
    expect((await status(r, { status: "fixed", resolvedVersion: "v2.25.0" })).status).toBe(200);
    expect(await errCode(await status(r, { status: "fixed", resolvedVersion: "v2.26.0" }))).toBe("feedback.bad_transition");
    expect(await errCode(await status(r, { status: "fixed", resolvedVersion: "next" }))).toBe("feedback.bad_transition");
  });

  it("refuses backward and terminal-to-terminal transitions", async () => {
    const r = await receiptOf();
    expect(await errCode(await status(r, { status: "in_progress" }))).toBe("feedback.bad_transition");
    await post(`/v1/admin/feedback/${r}/release`, {}, admin);
    await post(`/v1/admin/feedback/${r}/recorded`, { issueNumber: 1, issueUrl: "https://github.com/o/r/issues/1" }, admin);
    await status(r, { status: "wontfix" });
    expect(await errCode(await status(r, { status: "in_progress" }))).toBe("feedback.bad_transition");
    expect(await errCode(await status(r, { status: "fixed", resolvedVersion: "v1.0.0" }))).toBe("feedback.bad_transition");
  });

  it("hides held feedback from the converter until released", async () => {
    const links = Array.from({ length: 5 }, (_, i) => `https://spam${i}.test`).join(" ");
    const r = await receiptOf(submission({ body: links }));
    const items = async () => ((await (await call("/v1/admin/feedback/pending", { headers: admin })).json()) as { items: unknown[] }).items;
    expect(await items()).toHaveLength(0);
    expect(await errCode(await post(`/v1/admin/feedback/${r}/recorded`, { issueNumber: 2, issueUrl: "https://github.com/o/r/issues/2" }, admin))).toBe("feedback.bad_transition");
    expect((await post(`/v1/admin/feedback/${r}/release`, {}, admin)).status).toBe(200);
    expect(await items()).toHaveLength(1);
  });
});
