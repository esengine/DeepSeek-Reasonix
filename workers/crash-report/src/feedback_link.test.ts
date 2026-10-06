// @ts-expect-error Node 22+ provides node:sqlite; Worker production code does not import it.
import { DatabaseSync } from "node:sqlite";
// @ts-expect-error Test code uses Node to inspect the repository.
import { readFileSync, readdirSync } from "node:fs";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Env } from "./env";
import { verifyAssertion } from "./feedback_account";
import { installHash, installToken } from "./feedback_crypto";
import { installLevel } from "./feedback_level";
import { d1 } from "./feedback_testkit";
import { handleFeedbackRoute } from "./feedback_routes";
import feedbackMigrationSQL from "../migrate-feedback.sql?raw";
import triageMigrationSQL from "../migrate-feedback-triage.sql?raw";
import adoptionsMigrationSQL from "../migrate-feedback-adoptions.sql?raw";
import linksMigrationSQL from "../migrate-feedback-account-links.sql?raw";
import linksRollbackSQL from "../migrate-feedback-account-links.down.sql?raw";

const ADMIN = "admin-secret";
const TOKEN_SECRET = "token-secret";
const ACCOUNT_SECRET = "account-secret";
const enc = new TextEncoder();

let db: DatabaseSync;
let env: Env;
let ipAllowed = true;
let seq = 0;

const b64u = (bytes: Uint8Array) => btoa(String.fromCharCode(...bytes)).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
async function mac(secret: string, text: string): Promise<string> {
  const key = await crypto.subtle.importKey("raw", enc.encode(secret), { name: "HMAC", hash: "SHA-256" }, false, ["sign"]);
  return b64u(new Uint8Array(await crypto.subtle.sign("HMAC", key, enc.encode(text))));
}
async function assertion(sub: string | number, over: { exp?: number; aud?: string; secret?: string } = {}): Promise<string> {
  const exp = over.exp ?? Math.floor(Date.now() / 1000) + 300;
  const payload = b64u(enc.encode(JSON.stringify({ aud: over.aud ?? "feedback", exp, sub: String(sub) })));
  return `v1.${payload}.${await mac(over.secret ?? ACCOUNT_SECRET, `feedback-assertion:${payload}`)}`;
}

const installId = (n: number) => `install-${String(n).padStart(8, "0")}-aaaaaaaa`;
const proof = async (n: number) => ({ "x-install-id": installId(n), "x-install-token": await installToken(TOKEN_SECRET, installId(n)) });
const acct = async (sub: string | number, over: Parameters<typeof assertion>[1] = {}) => ({ "x-account-assertion": await assertion(sub, over) });
const hashOf = (n: number) => installHash(TOKEN_SECRET, installId(n));

const call = (path: string, init: RequestInit = {}) => handleFeedbackRoute(new Request(`https://crash.test${path}`, init), env) as Promise<Response | null>;
const send = async (method: string, path: string, headers: Record<string, string>, body?: unknown) =>
  (await call(path, { method, headers: { "content-type": "application/json", ...headers }, body: body === undefined ? undefined : JSON.stringify(body) })) as Response;
const code = async (r: Response) => ((await r.json()) as { error: { code: string } }).error.code;

const link = async (n: number, sub: string | number) => send("POST", "/v1/feedback/link", { ...(await proof(n)), ...(await acct(sub)) });
const unlink = async (n: number, sub: string | number) => send("DELETE", "/v1/feedback/link", { ...(await proof(n)), ...(await acct(sub)) });
const accountMine = async (sub: string | number, n?: number) => send("GET", "/v1/feedback/account/mine", { ...(await acct(sub)), ...(n === undefined ? {} : await proof(n)) });
const rows = (sql: string, ...a: unknown[]) => db.prepare(sql).all(...a) as any[];

async function report(n: number, body = "the composer freezes"): Promise<string> {
  const headers: Record<string, string> = { "content-type": "application/json" };
  const token = await installToken(TOKEN_SECRET, installId(n));
  if (rows("SELECT 1 FROM feedback WHERE install_hash = ?", await hashOf(n)).length > 0) headers["x-install-token"] = token;
  const res = await call("/v1/feedback", {
    method: "POST",
    headers,
    body: JSON.stringify({ idempotencyKey: `key-${++seq}-aaaaaaaa`, installId: installId(n), category: "bug", body, displayName: "Ada", env: { version: "v2.24.0", surface: "studio" } }),
  });
  expect(res!.status).toBe(201);
  const receipt = ((await res!.json()) as { receipt: string }).receipt;
  db.exec(`UPDATE feedback SET created_at = '2026-10-0${(seq % 9) + 1}T00:00:00.000Z' WHERE receipt = '${receipt}'`);
  return receipt;
}
const ask = (receipt: string) => send("POST", `/v1/admin/feedback/${receipt}/ask`, { authorization: `Bearer ${ADMIN}` }, { body: "Which version?" });
const reply = async (n: number, receipt: string) => send("POST", `/v1/feedback/${receipt}/reply`, await proof(n), { body: "v2.24.0" });

beforeEach(() => {
  db = new DatabaseSync(":memory:");
  for (const sql of [feedbackMigrationSQL, triageMigrationSQL, adoptionsMigrationSQL, linksMigrationSQL]) db.exec(sql);
  ipAllowed = true;
  seq = 0;
  env = {
    DB: d1(db),
    FEEDBACK_LIMITER: { limit: async () => ({ success: ipAllowed }) },
    FEEDBACK_TOKEN_SECRET: TOKEN_SECRET,
    FEEDBACK_ADMIN_TOKEN: ADMIN,
    FEEDBACK_ENABLED: "true",
    FEEDBACK_ACCOUNT_LINK: "true",
    FEEDBACK_ACCOUNT_SECRET: ACCOUNT_SECRET,
  } as unknown as Env;
});

afterEach(() => vi.restoreAllMocks());

describe("assertion contract", () => {
  const VECTOR = "v1.eyJhdWQiOiJmZWVkYmFjayIsImV4cCI6MTkwMDAwMDAwMCwic3ViIjoiNDIifQ.LdIhInX_RkQHYDYKzx1m0y90dGq432OfPhFsQeWUD0I";
  const AT = new Date(1_899_999_800_000);

  it("accepts the shared fixed vector and hashes the subject under its own label", async () => {
    const out = await verifyAssertion({ ...env, FEEDBACK_ACCOUNT_SECRET: "vector-secret" } as Env, VECTOR, AT);
    expect(out).toEqual({ accountHash: "1b195c7c60af4a567bb88df61db76a2317ad78174039b7ea2643bbaf92594a4f" });
  });

  it.each([
    ["expired", async () => assertion(42, { exp: Math.floor(Date.now() / 1000) - 1 })],
    ["lifetime beyond the cap", async () => assertion(42, { exp: Math.floor(Date.now() / 1000) + 7200 })],
    ["lifetime above the minted five minutes plus skew", async () => assertion(42, { exp: Math.floor(Date.now() / 1000) + 400 })],
    ["leading-zero subject", async () => assertion("042")],
    ["zero subject", async () => assertion("0")],
    ["wrong audience", async () => assertion(42, { aud: "dashboard" })],
    ["wrong key", async () => assertion(42, { secret: "other" })],
    ["non-numeric subject", async () => assertion("a@b.c")],
    ["tampered subject", async () => (await assertion(42)).replace(/\.[^.]+\./, `.${b64u(enc.encode(JSON.stringify({ aud: "feedback", exp: Math.floor(Date.now() / 1000) + 300, sub: "43" })))}.`)],
    ["garbage", async () => "nope"],
    ["empty", async () => ""],
  ])("refuses %s", async (_n, make) => {
    const res = await send("POST", "/v1/feedback/link", { ...(await proof(1)), "x-account-assertion": await make() });
    expect(res.status).toBe(401);
    expect(await code(res)).toBe("feedback.account_required");
    expect(rows("SELECT * FROM feedback_account_links")).toHaveLength(0);
  });
});

describe("POST /v1/feedback/link", () => {
  it("needs both the install proof and the account assertion", async () => {
    const installOnly = await send("POST", "/v1/feedback/link", await proof(1));
    expect(await code(installOnly)).toBe("feedback.account_required");
    const accountOnly = await send("POST", "/v1/feedback/link", await acct(7));
    expect(await code(accountOnly)).toBe("feedback.bad_token");
    const badToken = await send("POST", "/v1/feedback/link", { "x-install-id": installId(1), "x-install-token": "wrong", ...(await acct(7)) });
    expect(await code(badToken)).toBe("feedback.bad_token");
    const receiptOnly = await send("POST", "/v1/feedback/link", {}, { receipt: "FB-AAAA-AAAA" });
    expect(receiptOnly.status).toBe(401);
    expect(rows("SELECT * FROM feedback_account_links")).toHaveLength(0);
  });

  it("stores only keyed hashes and is idempotent for the same account", async () => {
    expect((await link(1, 42)).status).toBe(201);
    expect((await link(1, 42)).status).toBe(200);
    const stored = rows("SELECT * FROM feedback_account_links");
    expect(stored).toHaveLength(1);
    expect(stored[0].install_hash).toBe(await hashOf(1));
    expect(stored[0].account_hash).toBe("1b195c7c60af4a567bb88df61db76a2317ad78174039b7ea2643bbaf92594a4f");
    expect(JSON.stringify(stored)).not.toContain(installId(1));
    expect(JSON.stringify(stored)).not.toContain('"42"');
  });

  it("refuses an install already linked to another account without moving it", async () => {
    await link(1, 42);
    const res = await link(1, 43);
    expect(res.status).toBe(409);
    const text = await res.text();
    expect(text).toContain("feedback.link_conflict");
    expect(text).not.toMatch(/[0-9a-f]{32}/);
    expect(rows("SELECT account_hash FROM feedback_account_links")[0].account_hash).toBe("1b195c7c60af4a567bb88df61db76a2317ad78174039b7ea2643bbaf92594a4f");
  });

  it("caps an account at ten installs and still accepts a re-link at the cap", async () => {
    for (let n = 1; n <= 10; n++) expect((await link(n, 42)).status).toBe(201);
    const over = await link(11, 42);
    expect(over.status).toBe(409);
    expect(await code(over)).toBe("feedback.link_limit");
    expect(rows("SELECT * FROM feedback_account_links")).toHaveLength(10);
    expect((await link(3, 42)).status).toBe(200);
    expect((await link(11, 43)).status).toBe(201);
  });

  it("answers a conflict identically whichever other account holds the install", async () => {
    await link(1, 42);
    const conflict = await link(1, 43);
    const other = await link(1, 44);
    expect(await conflict.text()).toBe(await other.text());
  });
});

describe("unlink", () => {
  it("removes only the caller's own link and answers identically when nothing matched", async () => {
    await link(1, 42);
    const stranger = await unlink(1, 43);
    const missing = await unlink(2, 42);
    expect(stranger.status).toBe(200);
    expect(await stranger.text()).toBe(await missing.text());
    expect(rows("SELECT * FROM feedback_account_links")).toHaveLength(1);
    const own = await unlink(1, 42);
    expect(own.status).toBe(200);
    expect(rows("SELECT * FROM feedback_account_links")).toHaveLength(0);
  });

  it("requires both proofs on the install route", async () => {
    await link(1, 42);
    expect(await code(await send("DELETE", "/v1/feedback/link", await proof(1)))).toBe("feedback.account_required");
    expect(await code(await send("DELETE", "/v1/feedback/link", await acct(42)))).toBe("feedback.bad_token");
    expect(rows("SELECT * FROM feedback_account_links")).toHaveLength(1);
  });

  it("frees a lost install by the listed short id with the assertion alone", async () => {
    await link(1, 42);
    await link(2, 42);
    const short = (await hashOf(1)).slice(0, 12);
    const stranger = await send("DELETE", `/v1/feedback/link/${short}`, await acct(43));
    expect(stranger.status).toBe(200);
    expect(rows("SELECT * FROM feedback_account_links")).toHaveLength(2);
    const own = await send("DELETE", `/v1/feedback/link/${short}`, await acct(42));
    expect(await own.text()).toBe(await stranger.text());
    expect(rows("SELECT install_hash FROM feedback_account_links").map((r) => r.install_hash)).toEqual([await hashOf(2)]);
    expect(await code(await send("DELETE", "/v1/feedback/link/zzz", await acct(42)))).toBe("feedback.invalid");
    expect(await code(await send("DELETE", `/v1/feedback/link/${short}`, {}))).toBe("feedback.account_required");
  });

  it("lets a freed slot be reused at the cap", async () => {
    for (let n = 1; n <= 10; n++) await link(n, 42);
    await send("DELETE", `/v1/feedback/link/${(await hashOf(4)).slice(0, 12)}`, await acct(42));
    expect((await link(11, 42)).status).toBe(201);
  });
});

describe("GET /v1/feedback/link", () => {
  it("lists only the caller's installs by short id and marks the current one", async () => {
    await link(1, 42);
    await link(2, 42);
    await link(3, 43);
    const res = await send("GET", "/v1/feedback/link", { ...(await acct(42)), ...(await proof(2)) });
    const body = (await res.json()) as { installs: { id: string; linkedAt: string; current: boolean }[] };
    expect(body.installs.map((i) => i.id).sort()).toEqual([(await hashOf(1)).slice(0, 12), (await hashOf(2)).slice(0, 12)].sort());
    expect(body.installs.find((i) => i.current)?.id).toBe((await hashOf(2)).slice(0, 12));
    expect(JSON.stringify(body)).not.toContain(await hashOf(1));
    const anon = (await (await send("GET", "/v1/feedback/link", await acct(42))).json()) as typeof body;
    expect(anon.installs.every((i) => !i.current)).toBe(true);
    expect(await code(await send("GET", "/v1/feedback/link", { ...(await acct(42)), "x-install-id": installId(2), "x-install-token": "bad" }))).toBe("feedback.bad_token");
  });
});

describe("GET /v1/feedback/account/mine", () => {
  it("unions only the linked installs, ordered newest first, never across accounts", async () => {
    const a = await report(1, "from a");
    const b = await report(2, "from b");
    const c = await report(3, "from stranger");
    const d = await report(4, "unlinked of same person");
    await link(1, 42);
    await link(2, 42);
    await link(3, 43);
    const items = ((await (await accountMine(42)).json()) as { items: { receipt: string; linkedHere: boolean }[] }).items;
    expect(items.map((i) => i.receipt).sort()).toEqual([a, b].sort());
    expect(items.map((i) => i.receipt)).not.toContain(c);
    expect(items.map((i) => i.receipt)).not.toContain(d);
    const other = ((await (await accountMine(43)).json()) as { items: { receipt: string }[] }).items;
    expect(other.map((i) => i.receipt)).toEqual([c]);
    expect(((await (await accountMine(99)).json()) as { items: unknown[] }).items).toEqual([]);
  });

  it("carries replies, projects held and rejected as today and marks linkedHere only for the proven install", async () => {
    const a = await report(1);
    const b = await report(2);
    await link(1, 42);
    await link(2, 42);
    await ask(a);
    db.exec(`UPDATE feedback SET status = 'rejected' WHERE receipt = '${b}'`);
    const res = await accountMine(42, 1);
    const body = (await res.json()) as { profile: { level: number }; items: { receipt: string; status: string; needsInput: boolean; replies: { author: string }[]; linkedHere: boolean }[] };
    const byReceipt = Object.fromEntries(body.items.map((i) => [i.receipt, i]));
    expect(byReceipt[a]).toMatchObject({ needsInput: true, linkedHere: true });
    expect(byReceipt[a].replies.map((r) => r.author)).toEqual(["maintainer"]);
    expect(byReceipt[b]).toMatchObject({ status: "closed", linkedHere: false });
    expect(body.profile.level).toBe(0);
    expect(((await (await accountMine(42)).json()) as { items: { linkedHere: boolean }[] }).items.every((i) => !i.linkedHere)).toBe(true);
  });

  it("returns the same item shape as /mine plus linkedHere", async () => {
    await report(1);
    await link(1, 42);
    const own = (await (await call("/v1/feedback/mine", { headers: await proof(1) }))!.json()) as { items: Record<string, unknown>[] };
    const union = (await (await accountMine(42, 1)).json()) as { items: Record<string, unknown>[] };
    expect(Object.keys(union.items[0]).sort()).toEqual([...Object.keys(own.items[0]), "linkedHere"].sort());
  });

  it("limits the union to fifty items", async () => {
    await link(1, 42);
    const h = await hashOf(1);
    for (let i = 0; i < 55; i++) {
      db.prepare("INSERT INTO feedback (idempotency_key, receipt, install_hash, category, body, display_name, contact, env_json, attachments_json, status, created_at, updated_at) VALUES (?, ?, ?, 'bug', 'x', 'A', '', '{}', '[]', 'received', ?, ?)")
        .run(`idem-${i}-aaaaaaaa`, `FB-${String(i).padStart(4, "A")}-AAAA`, h, `2026-10-01T00:00:${String(i).padStart(2, "0")}.000Z`, `2026-10-01T00:00:${String(i).padStart(2, "0")}.000Z`);
    }
    expect(((await (await accountMine(42)).json()) as { items: unknown[] }).items).toHaveLength(50);
  });

  it("leaves /v1/feedback/mine install-keyed after linking", async () => {
    const a = await report(1);
    await report(2);
    await link(1, 42);
    await link(2, 42);
    const mine = ((await (await call("/v1/feedback/mine", { headers: await proof(1) }))!.json()) as { items: { receipt: string }[] }).items;
    expect(mine.map((i) => i.receipt)).toEqual([a]);
  });
});

describe("replying from a linked sibling", () => {
  it("allows it, charges the calling install and uses one ownership rule", async () => {
    const receipt = await report(1);
    await ask(receipt);
    expect((await reply(2, receipt)).status).toBe(409);
    await link(1, 42);
    await link(2, 42);
    const res = await reply(2, receipt);
    expect(res.status).toBe(201);
    const buckets = rows("SELECT bucket FROM feedback_quota WHERE bucket LIKE 'rh:%'").map((r) => r.bucket as string);
    expect(buckets).toHaveLength(1);
    expect(buckets[0].startsWith(`rh:${await hashOf(2)}:`)).toBe(true);
    expect(rows("SELECT status FROM feedback WHERE receipt = ?", receipt)[0].status).toBe("held");
    expect(rows("SELECT author FROM feedback_replies WHERE author = 'user'")).toHaveLength(1);
  });

  it("refuses an unlinked install and one linked to a different account with not_replyable", async () => {
    const receipt = await report(1);
    await ask(receipt);
    await link(1, 42);
    await link(2, 43);
    expect(await code(await reply(2, receipt))).toBe("feedback.not_replyable");
    expect(await code(await reply(3, receipt))).toBe("feedback.not_replyable");
    await link(3, 42);
    await unlink(3, 42);
    expect(await code(await reply(3, receipt))).toBe("feedback.not_replyable");
  });

  it("keeps the per-item cap across siblings", async () => {
    const receipt = await report(1);
    await link(1, 42);
    await link(2, 42);
    await ask(receipt);
    for (let i = 0; i < 10; i++) {
      db.prepare("INSERT INTO feedback_replies (receipt, author, body, handled, created_at) VALUES (?, 'user', 'x', 0, '2026-10-01T00:00:00.000Z')").run(receipt);
    }
    expect(await code(await reply(2, receipt))).toBe("feedback.reply_limit");
  });
});

describe("levels, trust and blocks stay per install", () => {
  const now = new Date("2026-10-03T12:00:00.000Z");
  const trust = async (n: number) => db.prepare("INSERT INTO feedback_trust (install_hash, created_at, expires_at) VALUES (?, '2026-10-01T00:00:00.000Z', '2027-01-01T00:00:00.000Z')").run(await hashOf(n));
  const credit = async (n: number, key: string) =>
    db.prepare("INSERT INTO feedback_adoptions (install_hash, item_key, receipt, version, adopted_at) VALUES (?, ?, ?, 'v2.24.0', '2026-10-01T00:00:00.000Z')").run(await hashOf(n), key, `FB-CRED-${key}`);

  it("keeps legacy_active unchanged by linking and still narrows on its own first credit", async () => {
    await trust(1);
    expect((await installLevel(env, await hashOf(1), now)).trustState).toBe("legacy_active");
    await credit(2, "A1");
    await link(1, 42);
    await link(2, 42);
    const after = await installLevel(env, await hashOf(1), now);
    expect(after).toMatchObject({ trustState: "legacy_active", adoptedCount: 0, level: 0 });
    await credit(1, "B1");
    expect((await installLevel(env, await hashOf(1), now)).trustState).toBe("active");
  });

  it("keeps a revoked install revoked through link and unlink", async () => {
    db.prepare("INSERT INTO feedback_level_state (install_hash, high_water_count, revoked_at, updated_at) VALUES (?, 0, '2026-10-02T00:00:00.000Z', '2026-10-02T00:00:00.000Z')").run(await hashOf(1));
    await link(1, 42);
    expect((await installLevel(env, await hashOf(1), now)).trustState).toBe("revoked");
    await unlink(1, 42);
    expect((await installLevel(env, await hashOf(1), now)).trustState).toBe("revoked");
    expect(rows("SELECT * FROM feedback_level_state")).toHaveLength(1);
  });

  it("does not let a blocked install's block extend to the account or its siblings", async () => {
    await link(1, 42);
    await link(2, 42);
    db.prepare("INSERT INTO feedback_blocks (target, reason, created_at, expires_at) VALUES (?, 'x', '2026-10-01T00:00:00.000Z', NULL)").run(`install:${await hashOf(1)}`);
    expect((await installLevel(env, await hashOf(2), now)).heldEligible).toBe(false);
    expect(rows("SELECT * FROM feedback_blocks")).toHaveLength(1);
  });
});

describe("blocks follow the report's install", () => {
  const block = async (n: number, expires: string | null = null) =>
    db.prepare("INSERT INTO feedback_blocks (target, reason, created_at, expires_at) VALUES (?, 'x', '2026-10-01T00:00:00.000Z', ?)").run(`install:${await hashOf(n)}`, expires);
  const userReplies = () => rows("SELECT * FROM feedback_replies WHERE author = 'user'").length;

  it("does not let an unblocked sibling reply on a blocked install's report", async () => {
    const receipt = await report(1);
    await ask(receipt);
    await link(1, 42);
    await link(2, 42);
    await block(1);
    expect((await reply(1, receipt)).status).toBe(429);
    const res = await reply(2, receipt);
    expect(res.status).toBe(409);
    expect(await code(res)).toBe("feedback.not_replyable");
    expect(userReplies()).toBe(0);
  });

  it("restores sibling replies once the block lapses or is lifted, and ignores an expired block", async () => {
    const receipt = await report(1);
    await ask(receipt);
    await link(1, 42);
    await link(2, 42);
    await block(1, "2026-10-02T00:00:00.000Z");
    expect((await reply(2, receipt)).status).toBe(201);
    await ask(receipt);
    db.exec("DELETE FROM feedback_blocks");
    await block(1);
    expect((await reply(2, receipt)).status).toBe(409);
    db.exec("DELETE FROM feedback_blocks");
    expect((await reply(2, receipt)).status).toBe(201);
  });

  it("still lets an unblocked install reply on its own reports while a sibling is blocked", async () => {
    const own = await report(2);
    await ask(own);
    await link(1, 42);
    await link(2, 42);
    await block(1);
    expect((await reply(2, own)).status).toBe(201);
  });

  it("refuses to link a blocked install without revealing the block", async () => {
    await block(1);
    const res = await link(1, 42);
    expect(res.status).toBe(429);
    expect(await code(res)).toBe("feedback.rate_limited");
    expect(rows("SELECT * FROM feedback_account_links")).toHaveLength(0);
    expect((await link(2, 42)).status).toBe(201);
  });
});

describe("fail closed and abuse limits", () => {
  it("refuses with account_unavailable when the limiter binding is missing", async () => {
    env.FEEDBACK_LIMITER = undefined;
    for (const res of [await link(1, 42), await unlink(1, 42), await accountMine(42), await send("GET", "/v1/feedback/link", await acct(42))]) {
      expect(res.status).toBe(503);
      expect(await code(res)).toBe("feedback.account_unavailable");
    }
    expect(rows("SELECT * FROM feedback_account_links")).toHaveLength(0);
  });

  it("honours the per-IP limiter on every new route", async () => {
    ipAllowed = false;
    expect((await link(1, 42)).status).toBe(429);
    expect((await accountMine(42)).status).toBe(429);
    expect(rows("SELECT * FROM feedback_account_links")).toHaveLength(0);
  });

  it("limits link and unlink writes per account per day without an install-count signal", async () => {
    let limited = 0;
    for (let i = 0; i < 40; i++) {
      const res = await (i % 2 ? unlink(1, 42) : link(1, 42));
      if (res.status === 429) {
        limited++;
        expect(await code(res)).toBe("feedback.rate_limited");
      }
    }
    expect(limited).toBeGreaterThan(0);
    expect((await link(2, 43)).status).toBe(201);
  });

  it("limits link writes per install per hour across accounts", async () => {
    let limited = 0;
    for (let i = 0; i < 14; i++) if ((await link(1, 100 + i)).status === 429) limited++;
    expect(limited).toBeGreaterThan(0);
  });

  it("limits reads per account per hour", async () => {
    let limited = 0;
    for (let i = 0; i < 130; i++) if ((await accountMine(42)).status === 429) limited++;
    expect(limited).toBeGreaterThan(0);
    expect((await accountMine(43)).status).toBe(200);
  });

  it("rejects wrong methods with the standard code", async () => {
    expect(await code(await send("PUT", "/v1/feedback/link", {}))).toBe("feedback.method_not_allowed");
    expect(await code(await send("POST", "/v1/feedback/account/mine", {}))).toBe("feedback.method_not_allowed");
  });
});

describe("kill switch", () => {
  const paths: [string, string][] = [
    ["POST", "/v1/feedback/link"],
    ["DELETE", "/v1/feedback/link"],
    ["GET", "/v1/feedback/link"],
    ["DELETE", "/v1/feedback/link/0123456789ab"],
    ["GET", "/v1/feedback/account/mine"],
  ];

  it.each([["unset", undefined], ["false", "false"], ["empty", ""]])("makes every new route not exist when the flag is %s", async (_n, value) => {
    env.FEEDBACK_ACCOUNT_LINK = value;
    for (const [method, path] of paths) {
      expect(await call(path, { method, headers: { ...(await proof(1)), ...(await acct(42)) } })).toBeNull();
    }
    expect(rows("SELECT * FROM feedback_account_links")).toHaveLength(0);
  });

  it("makes the routes not exist while the shared secret is missing", async () => {
    env.FEEDBACK_ACCOUNT_SECRET = undefined;
    for (const [method, path] of paths) expect(await call(path, { method })).toBeNull();
  });

  it("keeps mine and reply on the install-only path even when link rows exist", async () => {
    const receipt = await report(1);
    await ask(receipt);
    await link(1, 42);
    await link(2, 42);
    env.FEEDBACK_ACCOUNT_LINK = "false";
    expect(await code(await reply(2, receipt))).toBe("feedback.not_replyable");
    const own = ((await (await call("/v1/feedback/mine", { headers: await proof(2) }))!.json()) as { items: unknown[] }).items;
    expect(own).toEqual([]);
    expect((await reply(1, receipt)).status).toBe(201);
  });

  it("issues the same ownership SQL as before when off", async () => {
    const seen: string[] = [];
    const real = env.DB.prepare.bind(env.DB);
    env.DB = { ...env.DB, prepare: (sql: string) => (seen.push(sql), real(sql)), batch: env.DB.batch.bind(env.DB) } as unknown as D1Database;
    env.FEEDBACK_ACCOUNT_LINK = "false";
    const receipt = await report(1);
    await ask(receipt);
    await reply(1, receipt);
    expect(seen.some((s) => s.includes("feedback_account_links"))).toBe(false);
  });
});

describe("account erasure", () => {
  const erase = async (sub: string, secret = ACCOUNT_SECRET, tamper = false) => {
    const body = JSON.stringify({ sub });
    const sig = await mac(secret, `feedback-account-erase:${tamper ? body + " " : body}`);
    return (await call("/v1/feedback/account/erase", { method: "POST", headers: { "x-erase-signature": sig, "content-type": "application/json" }, body })) as Response;
  };

  it("matches the shared vector and removes only that account's links, idempotently", async () => {
    await link(1, 42);
    await link(2, 42);
    await link(3, 43);
    const body = JSON.stringify({ sub: "42" });
    expect(await mac("vector-secret", `feedback-account-erase:${body}`)).toBe("KoDfcAUpCN0XCPai7d56pgUHNTQcqXCe6SDxiAOKDw0");
    expect((await erase("42")).status).toBe(200);
    expect(rows("SELECT install_hash FROM feedback_account_links").map((r) => r.install_hash)).toEqual([await hashOf(3)]);
    expect((await erase("42")).status).toBe(200);
    expect(rows("SELECT * FROM feedback_account_links")).toHaveLength(1);
  });

  it("refuses a bad or missing signature and a malformed body without deleting", async () => {
    await link(1, 42);
    expect((await erase("42", "wrong")).status).toBe(401);
    expect((await erase("42", ACCOUNT_SECRET, true)).status).toBe(401);
    const none = (await call("/v1/feedback/account/erase", { method: "POST", body: JSON.stringify({ sub: "42" }) })) as Response;
    expect(none.status).toBe(401);
    const bad = JSON.stringify({ sub: "a@b.c" });
    const res = (await call("/v1/feedback/account/erase", { method: "POST", headers: { "x-erase-signature": await mac(ACCOUNT_SECRET, `feedback-account-erase:${bad}`) }, body: bad })) as Response;
    expect(res.status).toBe(400);
    expect(rows("SELECT * FROM feedback_account_links")).toHaveLength(1);
  });

  it("still works while the link flag is off, and leaves only install-keyed data afterwards", async () => {
    const receipt = await report(1);
    await ask(receipt);
    await link(1, 42);
    await link(2, 42);
    env.FEEDBACK_ACCOUNT_LINK = "false";
    expect((await erase("42")).status).toBe(200);
    env.FEEDBACK_ACCOUNT_LINK = "true";
    expect(((await (await accountMine(42)).json()) as { items: unknown[] }).items).toEqual([]);
    expect(await code(await reply(2, receipt))).toBe("feedback.not_replyable");
    expect(rows("SELECT receipt FROM feedback")).toHaveLength(1);
  });

  it("does not exist without the shared secret", async () => {
    env.FEEDBACK_ACCOUNT_SECRET = undefined;
    expect(await call("/v1/feedback/account/erase", { method: "POST", body: "{}" })).toBeNull();
  });
});

describe("privacy", () => {
  it("never writes ids, tokens, assertions, subjects or account hashes to the console, on any path", async () => {
    const spies = (["log", "info", "warn", "error", "debug"] as const).map((k) => vi.spyOn(console, k).mockImplementation(() => {}));
    const header = await acct(42);
    const receipt = await report(1);
    await ask(receipt);
    await link(1, 42);
    await link(2, 42);
    await link(1, 43);
    await accountMine(42, 1);
    await reply(2, receipt);
    await send("GET", "/v1/feedback/link", { ...header, ...(await proof(1)) });
    await unlink(1, 42);
    const body = JSON.stringify({ sub: "42" });
    await call("/v1/feedback/account/erase", { method: "POST", headers: { "x-erase-signature": await mac(ACCOUNT_SECRET, `feedback-account-erase:${body}`) }, body });
    await call("/v1/feedback/account/erase", { method: "POST", headers: { "x-erase-signature": "bad" }, body });
    const printed = JSON.stringify(spies.flatMap((s) => s.mock.calls));
    const accountHash42 = "1b195c7c60af4a567bb88df61db76a2317ad78174039b7ea2643bbaf92594a4f";
    for (const secret of [installId(1), await installToken(TOKEN_SECRET, installId(1)), header["x-account-assertion"], await hashOf(1), accountHash42, ACCOUNT_SECRET, TOKEN_SECRET]) {
      expect(printed).not.toContain(secret);
    }
  });

  const sources = () =>
    (readdirSync("src", { recursive: true }) as string[])
      .filter((f) => f.endsWith(".ts") && !f.endsWith(".test.ts"))
      .map((f) => ({ file: f, text: readFileSync(`src/${f}`, "utf8") as string }));

  it("mentions the link table only in the link code, anywhere under src", () => {
    const users = sources().filter((f) => f.text.includes("account_links")).map((f) => f.file).sort();
    expect(users).toEqual(["feedback_link.ts", "feedback_ownership.ts"]);
  });

  it("lets only the routing, reply and quota code reach the link and ownership modules", () => {
    const importers = sources()
      .filter((f) => /from "\.\/feedback_(link|ownership)"|from "\.\.?\/[^"]*feedback_(link|ownership)"/.test(f.text))
      .map((f) => f.file)
      .sort();
    expect(importers).toEqual(["feedback_quota.ts", "feedback_reply.ts", "feedback_routes.ts"]);
  });
});

describe("migration", () => {
  const tables = () => rows("SELECT name, sql FROM sqlite_master WHERE name NOT LIKE 'sqlite_%' AND name NOT LIKE 'feedback_account_links%' ORDER BY name");

  it("is re-runnable and leaves every existing table and index untouched", () => {
    const before = tables();
    db.exec(linksMigrationSQL);
    expect(tables()).toEqual(before);
    expect(rows("SELECT name FROM sqlite_master WHERE name LIKE 'feedback_account_links%' ORDER BY name").map((r) => r.name)).toEqual(["feedback_account_links", "feedback_account_links_account"]);
  });

  it("rolls back to exactly the previous schema, and old behaviour keeps working without the table", async () => {
    const before = tables();
    db.exec(linksRollbackSQL);
    expect(tables()).toEqual(before);
    expect(rows("SELECT name FROM sqlite_master WHERE name LIKE 'feedback_account_links%'")).toEqual([]);
    env.FEEDBACK_ACCOUNT_LINK = "false";
    const receipt = await report(1);
    await ask(receipt);
    expect((await reply(1, receipt)).status).toBe(201);
    db.exec(linksMigrationSQL);
    env.FEEDBACK_ACCOUNT_LINK = "true";
    expect((await link(1, 42)).status).toBe(201);
  });
});

describe("deploy contract", () => {
  it("applies and verifies the link table after the adoption schema and before the Worker deploys", () => {
    const workflow = readFileSync("../../.github/workflows/deploy-crash-worker.yml", "utf8") as string;
    const step = workflow.indexOf("Apply and verify feedback account link D1 migration");
    expect(step).toBeGreaterThan(workflow.indexOf("Apply and verify feedback adoption D1 migration"));
    expect(step).toBeLessThan(workflow.indexOf("npx wrangler deploy"));
    const body = workflow.slice(step, workflow.indexOf("- name: Apply Studio telemetry schema"));
    expect(body).toContain("--file=migrate-feedback-account-links.sql");
    for (const name of ["feedback_account_links", "feedback_account_links_account"]) expect(body).toContain(name);
    expect(body).toContain("missing $name after migration");
    expect(workflow).toContain("FEEDBACK_ACCOUNT_SECRET: ${{ secrets.FEEDBACK_ACCOUNT_SECRET }}");
  });

  it("ships the kill switch off by default", () => {
    const toml = readFileSync("wrangler.toml", "utf8") as string;
    expect(toml).toMatch(/^FEEDBACK_ACCOUNT_LINK = "false"$/m);
  });
});
