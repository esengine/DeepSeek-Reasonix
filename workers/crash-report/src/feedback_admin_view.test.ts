// @ts-expect-error Node 22+ provides node:sqlite; Worker production code does not import it.
import { DatabaseSync } from "node:sqlite";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Env } from "./env";
import { APP_JS } from "./feedback_admin_ui_js";
import { d1, fakeR2 } from "./feedback_testkit";
import { handleFeedbackRoute } from "./feedback_routes";
import feedbackMigrationSQL from "../migrate-feedback.sql?raw";
import triageMigrationSQL from "../migrate-feedback-triage.sql?raw";
import adoptionsMigrationSQL from "../migrate-feedback-adoptions.sql?raw";

const ADMIN = "admin-secret-value";
const admin = { authorization: `Bearer ${ADMIN}` };
const PNG_B64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAAC0lEQVR4nGNgAAIAAAUAAXpeqz8AAAAASUVORK5CYII=";

let env: Env;
let raw: DatabaseSync;
let seq = 0;
let tokens = new Map<string, string>();

beforeEach(() => {
  raw = new DatabaseSync(":memory:");
  raw.exec(feedbackMigrationSQL);
  raw.exec(triageMigrationSQL);
  raw.exec(adoptionsMigrationSQL);
  tokens = new Map();
  env = {
    DB: d1(raw),
    TELEMETRY_RAW: fakeR2().bucket,
    FEEDBACK_LIMITER: { limit: async () => ({ success: true }) },
    FEEDBACK_TOKEN_SECRET: "token-secret",
    FEEDBACK_ADMIN_TOKEN: ADMIN,
    FEEDBACK_ENABLED: "true",
  } as unknown as Env;
});

afterEach(() => vi.useRealTimers());

const call = (path: string, init: RequestInit = {}) => handleFeedbackRoute(new Request(`https://crash.test${path}`, init), env) as Promise<Response>;
const get = (path: string, headers: Record<string, string> = admin) => call(path, { headers });
const json = async <T = any>(r: Response) => (await r.json()) as T;

async function submit(over: Record<string, unknown> = {}): Promise<string> {
  raw.exec("DELETE FROM feedback_quota WHERE bucket NOT LIKE 'af:%'");
  const installId = (over.installId as string) ?? "install-aaaaaaaaaaaaaaaa";
  const res = await call("/v1/feedback", {
    method: "POST",
    headers: { "content-type": "application/json", ...(tokens.has(installId) ? { "x-install-token": tokens.get(installId)! } : {}) },
    body: JSON.stringify({ idempotencyKey: `key-${++seq}-aaaaaaaa`, installId, category: "bug", body: "the composer freezes", displayName: "Ada", env: { version: "v2.24.0", os: "darwin", osVersion: "15.1", arch: "arm64" }, ...over }),
  });
  const out = await json(res);
  tokens.set(installId, out.installToken);
  return out.receipt as string;
}
const act = (receipt: string, action: string, body: unknown = {}) =>
  call(`/v1/admin/feedback/${receipt}/${action}`, { method: "POST", headers: { ...admin, "content-type": "application/json" }, body: JSON.stringify(body) });
const list = async (qs = "") => json<{ items: any[]; nextBefore: number | null }>(await get(`/v1/admin/feedback/list${qs}`));
const hashOf = (receipt: string) => (raw.prepare("SELECT install_hash FROM feedback WHERE receipt = ?").get(receipt) as any).install_hash as string;

describe("admin list endpoint", () => {
  it("requires the admin bearer", async () => {
    await submit();
    expect((await get("/v1/admin/feedback/list", {})).status).toBe(401);
    expect((await get("/v1/admin/feedback/list", { authorization: "Bearer nope" })).status).toBe(401);
  });

  it("returns newest first with the summary fields and nothing private", async () => {
    const a = await submit({ contact: "ada@example.com", body: "first report" });
    const b = await submit({ body: "second report", attachments: [{ name: "a.png", contentType: "image/png", dataBase64: PNG_B64 }] });
    await act(b, "reply", { body: "thanks" });
    const { items, nextBefore } = await list();
    expect(items.map((i) => i.receipt)).toEqual([b, a]);
    expect(nextBefore).toBeNull();
    expect(items[0]).toMatchObject({ status: "held", category: "bug", displayName: "Ada", version: "v2.24.0", device: "darwin 15.1 arm64", hasImages: true, imageCount: 1, replyCount: 1, snippet: "second report" });
    expect(items[1]).toMatchObject({ hasImages: false, replyCount: 0 });
    expect(items[0].createdAt).toBeTruthy();
    expect(JSON.stringify(items)).not.toContain("ada@example.com");
    expect(items[0]).not.toHaveProperty("contact");
    expect(items[0]).not.toHaveProperty("body");
  });

  it("pages with a cursor and reports the end", async () => {
    const r = [];
    for (let i = 0; i < 5; i++) r.push(await submit({ body: `report ${i}` }));
    const first = await list("?limit=2");
    expect(first.items.map((i) => i.receipt)).toEqual([r[4], r[3]]);
    expect(first.nextBefore).not.toBeNull();
    const second = await list(`?limit=2&before=${first.nextBefore}`);
    expect(second.items.map((i) => i.receipt)).toEqual([r[2], r[1]]);
    const last = await list(`?limit=2&before=${second.nextBefore}`);
    expect(last.items.map((i) => i.receipt)).toEqual([r[0]]);
    expect(last.nextBefore).toBeNull();
  });

  it("filters by status, kind, version, nickname and install", async () => {
    const a = await submit({ displayName: "Ada_100%", category: "idea", env: { version: "v2.23.0" } });
    const b = await submit({ installId: "install-bbbbbbbbbbbbbbbb", displayName: "Bob", env: { version: "v2.24.0" } });
    await act(a, "release");
    expect((await list("?status=received")).items.map((i) => i.receipt)).toEqual([a]);
    expect((await list("?status=held")).items.map((i) => i.receipt)).toEqual([b]);
    expect((await list("?category=idea")).items.map((i) => i.receipt)).toEqual([a]);
    expect((await list("?version=v2.24.0")).items.map((i) => i.receipt)).toEqual([b]);
    expect((await list("?nickname=ob")).items.map((i) => i.receipt)).toEqual([b]);
    expect((await list("?nickname=100%25")).items.map((i) => i.receipt)).toEqual([a]);
    expect((await list("?nickname=%25")).items.map((i) => i.receipt)).toEqual([a]);
    expect((await list("?nickname=_")).items.map((i) => i.receipt)).toEqual([a]);
    expect((await list(`?install=${hashOf(b).slice(0, 12)}`)).items.map((i) => i.receipt)).toEqual([b]);
    expect((await list("?install=install-aaaaaaaaaaaaaaaa")).items.map((i) => i.receipt)).toEqual([a]);
    expect((await list("?status=received&category=bug")).items).toHaveLength(0);
  });

  it("rejects malformed filters instead of ignoring them", async () => {
    for (const qs of ["?status=bogus", "?category=bogus", "?before=abc", "?before=0", "?install=zz", `?version=${"v".repeat(81)}`]) {
      expect((await get(`/v1/admin/feedback/list${qs}`)).status, qs).toBe(400);
    }
  });

  it("treats filter text as data, never as SQL", async () => {
    await submit();
    const res = await get(`/v1/admin/feedback/list?nickname=${encodeURIComponent("' OR 1=1 --")}`);
    expect(res.status).toBe(200);
    expect((await json(res)).items).toHaveLength(0);
  });
});

describe("admin page", () => {
  const PATHS = ["/admin/feedback", "/admin/feedback/app.js", "/admin/feedback/app.css"];

  it("serves the shell without a token and with no feedback data in it", async () => {
    await submit({ body: "secret user words" });
    const res = await call("/admin/feedback");
    expect(res.status).toBe(200);
    expect(res.headers.get("content-type")).toBe("text/html; charset=utf-8");
    const html = await res.text();
    expect(html).not.toContain("secret user words");
    expect(html).not.toContain(ADMIN);
  });

  it("sends the hardening headers on the page and its assets", async () => {
    for (const path of PATHS) {
      const res = await call(path);
      expect(res.status, path).toBe(200);
      const csp = res.headers.get("content-security-policy") ?? "";
      expect(csp, path).toContain("default-src 'none'");
      expect(csp, path).toContain("script-src 'self'");
      expect(csp, path).toContain("connect-src 'self'");
      expect(csp, path).toContain("frame-ancestors 'none'");
      expect(csp, path).toContain("form-action 'none'");
      expect(csp, path).not.toMatch(/unsafe-inline|unsafe-eval|\*|https?:/);
      expect(res.headers.get("x-robots-tag"), path).toContain("noindex");
      expect(res.headers.get("cache-control"), path).toBe("no-store");
      expect(res.headers.get("x-content-type-options"), path).toBe("nosniff");
      expect(res.headers.get("x-frame-options"), path).toBe("DENY");
      expect(res.headers.get("referrer-policy"), path).toBe("no-referrer");
    }
    expect((await call("/admin/feedback/app.js")).headers.get("content-type")).toContain("text/javascript");
    expect((await call("/admin/feedback/app.css")).headers.get("content-type")).toContain("text/css");
  });

  it("declares noindex in the markup and loads only same-origin assets", async () => {
    const html = await (await call("/admin/feedback")).text();
    expect(html).toContain('name="robots" content="noindex');
    expect(html).not.toMatch(/<script(?![^>]*\bsrc=)[^>]*>/);
    expect(html).not.toMatch(/\son[a-z]+=|style=|https?:\/\//);
  });

  it("answers only GET and HEAD, and does not serve unknown paths", async () => {
    for (const path of PATHS) expect((await call(path, { method: "POST" })).status, path).toBe(405);
    expect((await call("/admin/feedback", { method: "HEAD" })).status).toBe(200);
    expect(await call("/admin/feedback/", { method: "GET" })).toBeNull();
    expect(await call("/admin/feedback/secrets.js")).toBeNull();
    expect(await call("/admin/feedback/__proto__")).toBeNull();
  });

  it("never builds markup from data: the script has no HTML sinks", () => {
    for (const sink of ["innerHTML", "outerHTML", "insertAdjacentHTML", "document.write", "createContextualFragment", "eval(", "new Function", "setTimeout(\"", "srcdoc", "DOMParser", "javascript:"]) {
      expect(APP_JS, sink).not.toContain(sink);
    }
    expect(APP_JS).toContain("textContent");
    expect(APP_JS).toContain("createTextNode");
  });

  it("keeps the token in sessionStorage and the Authorization header only", () => {
    expect(APP_JS).toContain("sessionStorage");
    expect(APP_JS).not.toContain("localStorage");
    expect(APP_JS).not.toMatch(/document\.cookie|location\.(search|hash|href)|history\.(push|replace)State/);
    expect(APP_JS).not.toMatch(/token=|[?&]token/);
    expect(APP_JS).toContain('"Bearer " + token');
  });

  it("only links to github.com issues and sets rel on the link", () => {
    expect(APP_JS).toContain("https:\\/\\/github\\.com\\/");
    expect(APP_JS).toContain("noopener noreferrer");
  });
});

describe("admin auth hardening", () => {
  const withIp = (ip: string, token?: string) => ({ "cf-connecting-ip": ip, ...(token ? { authorization: `Bearer ${token}` } : {}) });
  const probe = (ip: string, token?: string) => get("/v1/admin/feedback/list", withIp(ip, token));

  it("takes the token from the header only, never the URL", async () => {
    await submit();
    expect((await get(`/v1/admin/feedback/list?token=${ADMIN}&authorization=${ADMIN}&access_token=${ADMIN}`, {})).status).toBe(401);
    expect((await get("/v1/admin/feedback/list", { authorization: ADMIN })).status).toBe(401);
    expect((await get("/v1/admin/feedback/list", { authorization: `Basic ${ADMIN}` })).status).toBe(401);
  });

  it("rejects near-miss tokens of any length", async () => {
    for (const [i, t] of [ADMIN.slice(0, -1), `${ADMIN}x`, ADMIN.toUpperCase(), "x".repeat(4096), ""].entries()) {
      expect((await probe(`198.51.100.${i + 1}`, t)).status, t.slice(0, 12)).toBe(401);
    }
    expect((await probe("198.51.100.50", ADMIN)).status).toBe(200);
  });

  it("locks a caller out after repeated wrong tokens, even for the right one", async () => {
    for (let i = 0; i < 5; i++) expect((await probe("192.0.2.1", `wrong-${i}`)).status).toBe(401);
    const locked = await probe("192.0.2.1", ADMIN);
    expect(locked.status).toBe(429);
    expect((await json(locked)).error.code).toBe("feedback.rate_limited");
    expect((await probe("192.0.2.1", "wrong-again")).status).toBe(429);
    expect((await probe("192.0.2.2", ADMIN)).status).toBe(200);
  });

  it("holds the limit under concurrent wrong tokens", async () => {
    const codes = await Promise.all(Array.from({ length: 40 }, (_, i) => probe("192.0.2.7", `wrong-${i}`).then((r) => r.status)));
    expect(codes.filter((c) => c === 401).length).toBeLessThanOrEqual(5);
    expect(codes.filter((c) => c === 429).length).toBeGreaterThanOrEqual(35);
    expect((await probe("192.0.2.7", ADMIN)).status).toBe(429);
  });

  it("does not spend the budget on correct tokens", async () => {
    for (let i = 0; i < 20; i++) expect((await probe("192.0.2.8", ADMIN)).status).toBe(200);
    for (let i = 0; i < 4; i++) expect((await probe("192.0.2.8", `wrong-${i}`)).status).toBe(401);
    expect((await probe("192.0.2.8", ADMIN)).status).toBe(200);
  });

  it("locks the whole admin surface, not just the list", async () => {
    for (let i = 0; i < 5; i++) await probe("192.0.2.3", `wrong-${i}`);
    const r = await submit();
    expect((await get(`/v1/admin/feedback/${r}`, withIp("192.0.2.3", ADMIN))).status).toBe(429);
    expect((await get("/v1/admin/feedback/held", withIp("192.0.2.3", ADMIN))).status).toBe(429);
  });

  it("does not count a request without any token", async () => {
    for (let i = 0; i < 8; i++) expect((await probe("192.0.2.4")).status).toBe(401);
    expect((await probe("192.0.2.4", ADMIN)).status).toBe(200);
  });

  it("lifts the lock when the window passes", async () => {
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date("2026-10-01T10:00:00Z"));
    for (let i = 0; i < 5; i++) await probe("192.0.2.5", `wrong-${i}`);
    expect((await probe("192.0.2.5", ADMIN)).status).toBe(429);
    vi.setSystemTime(new Date("2026-10-01T10:16:00Z"));
    expect((await probe("192.0.2.5", ADMIN)).status).toBe(200);
  });

  it("stores no raw address for the lockout", async () => {
    await probe("192.0.2.9", "wrong");
    const rows = raw.prepare("SELECT bucket FROM feedback_quota WHERE bucket LIKE 'af:%'").all() as { bucket: string }[];
    expect(rows).toHaveLength(1);
    expect(rows[0].bucket).not.toContain("192.0.2.9");
  });

  it("never writes the presented or configured token to the logs", async () => {
    const spies = (["log", "info", "warn", "error", "debug"] as const).map((k) => vi.spyOn(console, k).mockImplementation(() => {}));
    await probe("192.0.2.6", "presented-wrong-token");
    await probe("192.0.2.6", ADMIN);
    const logged = JSON.stringify(spies.flatMap((s) => s.mock.calls));
    spies.forEach((s) => s.mockRestore());
    expect(logged).not.toContain("presented-wrong-token");
    expect(logged).not.toContain(ADMIN);
  });

  it("serves an unreleased image to the admin only, as an inert typed download", async () => {
    const r = await submit({ attachments: [{ name: "a.png", contentType: "image/png", dataBase64: PNG_B64 }] });
    const key = (await json(await get(`/v1/admin/feedback/${r}`))).attachments[0].key as string;
    const path = `/v1/admin/feedback/${r}/attachments/${key}`;
    expect((await get(path, {})).status).toBe(401);
    const res = await get(path);
    expect(res.status).toBe(200);
    expect(res.headers.get("content-type")).toBe("image/png");
    expect(res.headers.get("x-content-type-options")).toBe("nosniff");
    expect(res.headers.get("cache-control")).toBe("no-store");
    expect(res.headers.get("content-security-policy")).toContain("sandbox");
  });

  it("marks JSON responses nosniff and uncached", async () => {
    const res = await get("/v1/admin/feedback/list");
    expect(res.headers.get("x-content-type-options")).toBe("nosniff");
    expect(res.headers.get("cache-control")).toBe("no-store");
  });
});
