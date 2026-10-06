// @ts-expect-error Node 22+ provides node:sqlite; Worker production code does not import it.
import { DatabaseSync } from "node:sqlite";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Env } from "./env";
import { d1 } from "./feedback_testkit";
import { handleFeedbackRoute } from "./feedback_routes";
import feedbackMigrationSQL from "../migrate-feedback.sql?raw";
import triageMigrationSQL from "../migrate-feedback-triage.sql?raw";
import adoptionsMigrationSQL from "../migrate-feedback-adoptions.sql?raw";

const ADMIN = "admin-secret";
const SECRET_TEXT = "my-private-bug-text-xyz";
let env: Env;
let pending: Promise<unknown>[];
let ctx: ExecutionContext;
let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  const db = new DatabaseSync(":memory:");
  db.exec(feedbackMigrationSQL);
  db.exec(triageMigrationSQL);
  db.exec(adoptionsMigrationSQL);
  pending = [];
  ctx = { waitUntil: (p: Promise<unknown>) => void pending.push(p) } as unknown as ExecutionContext;
  fetchMock = vi.fn(async () => new Response(null, { status: 202 }));
  vi.stubGlobal("fetch", fetchMock);
  env = {
    DB: d1(db),
    FEEDBACK_TOKEN_SECRET: "token-secret",
    FEEDBACK_ADMIN_TOKEN: ADMIN,
    FEEDBACK_ENABLED: "true",
    OPS_EVENTS_URL: "https://ops.test",
    OPS_EMIT_TOKEN: "emit-secret",
  } as unknown as Env;
});
afterEach(() => vi.unstubAllGlobals());

let seq = 0;
const call = (path: string, init: RequestInit = {}) => handleFeedbackRoute(new Request(`https://crash.test${path}`, init), env, ctx) as Promise<Response>;
const post = (path: string, body: unknown, headers: Record<string, string> = {}) =>
  call(path, { method: "POST", body: JSON.stringify(body), headers: { "content-type": "application/json", ...headers } });
const submission = () => ({
  idempotencyKey: `key-${++seq}-aaaaaaaa`,
  installId: `install-${seq}-aaaaaaaaaaaa`,
  category: "bug",
  body: SECRET_TEXT,
  displayName: "Alice Example",
  contact: "alice@example.com",
  env: { version: "1.0.0" },
  attachments: [],
});
const flush = () => Promise.all(pending);
const sent = () => fetchMock.mock.calls.map(([url, init]) => ({ url: String(url), init: init as RequestInit, body: JSON.parse(String((init as RequestInit).body)) }));

describe("feedback ops emission", () => {
  it("emits a compact submitted event with no user text", async () => {
    const res = await post("/v1/feedback", submission());
    expect(res.status).toBe(201);
    const { receipt } = (await res.json()) as { receipt: string };
    await flush();
    const [call1] = sent();
    expect(call1.url).toBe("https://ops.test/emit");
    expect((call1.init.headers as Record<string, string>).authorization).toBe("Bearer emit-secret");
    expect(call1.body).toEqual({ src: "feedback", t: "submitted", title: "feedback submitted", extra: { receipt, category: "bug", status: expect.any(String) } });
    const raw = JSON.stringify(call1.body);
    for (const leak of [SECRET_TEXT, "alice@example.com", "Alice", "install-"]) expect(raw).not.toContain(leak);
  });

  it("sends the feedback payload byte for byte", async () => {
    const res = await post("/v1/feedback", submission());
    const { receipt } = (await res.json()) as { receipt: string };
    await flush();
    const [call1] = sent();
    const status = call1!.body.extra.status as string;
    expect(String((call1!.init as RequestInit).body)).toBe(
      `{"src":"feedback","t":"submitted","title":"feedback submitted","extra":{"receipt":"${receipt}","category":"bug","status":"${status}"}}`,
    );
  });

  it("emits on an admin status change", async () => {
    const created = await post("/v1/feedback", submission());
    const { receipt } = (await created.json()) as { receipt: string };
    await flush();
    fetchMock.mockClear();
    const admin = { authorization: `Bearer ${ADMIN}` };
    await post(`/v1/admin/feedback/${receipt}/release`, {}, admin);
    await post(`/v1/admin/feedback/${receipt}/recorded`, { issueNumber: 7, issueUrl: "https://github.com/o/r/issues/7" }, admin);
    const res = await post(`/v1/admin/feedback/${receipt}/status`, { status: "in_progress" }, admin);
    expect(res.status).toBe(200);
    await flush();
    const events = sent().map((s) => s.body);
    expect(events.map((e) => e.extra.status)).toEqual(["received", "recorded", "in_progress"]);
    expect(events.every((e) => e.t === "status" && e.extra.receipt === receipt)).toBe(true);
  });

  it("is a silent no-op without relay configuration", async () => {
    delete env.OPS_EVENTS_URL;
    const res = await post("/v1/feedback", submission());
    expect(res.status).toBe(201);
    await flush();
    expect(fetchMock).not.toHaveBeenCalled();
    delete env.OPS_EMIT_TOKEN;
    env.OPS_EVENTS_URL = "https://ops.test";
    expect((await post("/v1/feedback", submission())).status).toBe(201);
    await flush();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("never sends the token over a non-https relay URL", async () => {
    env.OPS_EVENTS_URL = "http://ops.test";
    expect((await post("/v1/feedback", submission())).status).toBe(201);
    await flush();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("does not change the response when the relay fails", async () => {
    fetchMock.mockRejectedValue(new Error("relay down"));
    const res = await post("/v1/feedback", submission());
    expect(res.status).toBe(201);
    await expect(flush()).resolves.toBeDefined();
  });

  it("does not wait for a hanging relay", async () => {
    fetchMock.mockImplementation((_url: string, init: RequestInit) => new Promise((_, reject) => init.signal?.addEventListener("abort", () => reject(new Error("aborted")))));
    const res = await post("/v1/feedback", submission());
    expect(res.status).toBe(201);
    expect((sent()[0].init.signal as AbortSignal).aborted).toBe(false);
  });
});
