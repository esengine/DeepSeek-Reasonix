import { afterEach, describe, expect, it, vi } from "vitest";
import registryApp from "../app";
import type { Bindings } from "../env";
import { sqliteD1 } from "../sqlite_d1.testkit";
import { resetRegistryGate } from "../../ops_registry";
import registrySchema from "../../../registry-schema.sql?raw";

const USERS: Record<string, { id: number; handle: string; role: string; emailVerified: boolean }> = {
  alice: { id: 7, handle: "alice", role: "member", emailVerified: true },
  root: { id: 1, handle: "root", role: "admin", emailVerified: true },
};
const EMAIL = "alice@example.com";
const TOKEN = "emit-secret";

function setup(opts: { relay?: "ok" | "down" | "off" } = {}) {
  resetRegistryGate();
  const relay = opts.relay ?? "ok";
  const emits: { url: string; headers: Record<string, string>; body: Record<string, unknown> }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (String(url).startsWith("https://ops.test")) {
        if (relay === "down") throw new Error("relay down");
        emits.push({ url: String(url), headers: init?.headers as Record<string, string>, body: JSON.parse(String(init?.body)) });
        return new Response(null, { status: 202 });
      }
      const auth = (init?.headers as Record<string, string> | undefined)?.authorization ?? "";
      const user = USERS[auth.replace(/^Bearer /, "")];
      return user ? Response.json({ user: { ...user, email: EMAIL } }) : new Response("no", { status: 401 });
    }),
  );
  const kit = sqliteD1(registrySchema);
  const env: Bindings = {
    DB: kit.db,
    ACCOUNTS_ORIGIN: "https://id.reasonix.test",
    APP_ORIGIN: "https://reasonix.test",
    ALLOWED_ORIGINS: "https://reasonix.test",
    ...(relay === "off" ? {} : { OPS_EVENTS_URL: "https://ops.test", OPS_EMIT_TOKEN: TOKEN }),
  };
  const pending: Promise<unknown>[] = [];
  const ctx = { waitUntil: (p: Promise<unknown>) => void pending.push(p), passThroughOnException() {} } as unknown as ExecutionContext;
  const publish = (as: string, body: Record<string, unknown>) =>
    registryApp.fetch(
      new Request("https://crash.reasonix.test/v1/packages", {
        method: "POST",
        headers: { authorization: `Bearer ${as}`, "content-type": "application/json" },
        body: JSON.stringify(body),
      }),
      env,
      ctx,
    );
  const flush = () => Promise.all(pending);
  return { ...kit, publish, flush, emits };
}

const pkg = (over: Record<string, unknown> = {}) => ({
  kind: "skill",
  name: "devkit",
  summary: "A handy skill",
  source: "https://github.com/o/r/tree/main/skills/devkit",
  ...over,
});

afterEach(() => vi.unstubAllGlobals());

describe("registry pending announcements", () => {
  it("announces a new pending package with metadata only", async () => {
    const t = setup();
    const res = await t.publish("alice", pkg({ description: "SECRET-DESC", manifest: "SECRET-MANIFEST" }));
    expect(res.status).toBe(201);
    await t.flush();
    expect(t.emits).toHaveLength(1);
    const [e] = t.emits;
    expect(e.url).toBe("https://ops.test/emit");
    expect(e.headers.authorization).toBe(`Bearer ${TOKEN}`);
    expect(e.body).toEqual({
      src: "registry",
      t: "pending",
      title: "registry submission pending",
      extra: { key: "alice/devkit@0.1.0", kind: "skill", source: "https://github.com/o/r/tree/main/skills/devkit", summary: "A handy skill" },
    });
    const raw = JSON.stringify(e.body);
    for (const leak of [EMAIL, "SECRET-DESC", "SECRET-MANIFEST", "alice-token", TOKEN]) expect(raw).not.toContain(leak);
  });

  it("announces a new version of an existing package that goes back to pending", async () => {
    const t = setup();
    t.sqlite.prepare("UPDATE packages SET status = 'active'").run();
    await t.publish("alice", pkg());
    await t.flush();
    t.emits.length = 0;
    t.sqlite.prepare("UPDATE packages SET status = 'active'").run();
    const res = await t.publish("alice", pkg({ version: "0.2.0" }));
    expect(res.status).toBe(200);
    await t.flush();
    expect(t.emits.map((e) => (e.body.extra as Record<string, string>).key)).toEqual(["alice/devkit@0.2.0"]);
  });

  it("does not announce an admin publish that is active at once", async () => {
    const t = setup();
    expect((await t.publish("root", pkg())).status).toBe(201);
    await t.flush();
    expect(t.emits).toHaveLength(0);
  });

  it("does not announce a private package", async () => {
    const t = setup();
    expect((await t.publish("alice", pkg({ visibility: "private" }))).status).toBe(201);
    await t.flush();
    expect(t.emits).toHaveLength(0);
  });

  it("does not announce a rejected duplicate version", async () => {
    const t = setup();
    await t.publish("alice", pkg({ version: "0.1.0" }));
    await t.flush();
    t.emits.length = 0;
    const res = await t.publish("alice", pkg({ version: "0.1.0" }));
    expect(res.status).toBe(409);
    await t.flush();
    expect(t.emits).toHaveLength(0);
  });

  it("does not announce a rejected submission", async () => {
    const t = setup();
    expect((await t.publish("alice", pkg({ source: "" }))).status).toBe(422);
    await t.flush();
    expect(t.emits).toHaveLength(0);
  });

  it("keeps the publish response identical when the relay is down", async () => {
    const t = setup({ relay: "down" });
    const res = await t.publish("alice", pkg());
    expect(res.status).toBe(201);
    const body = (await res.json()) as { created: boolean; version: string };
    expect(body).toMatchObject({ created: true, version: "0.1.0" });
    await expect(t.flush()).resolves.toBeDefined();
  });

  it("is a silent no-op without relay configuration", async () => {
    const t = setup({ relay: "off" });
    expect((await t.publish("alice", pkg())).status).toBe(201);
    await t.flush();
    expect(t.emits).toHaveLength(0);
  });

  it("leaves publishing unaffected once the registry event gate is saturated", async () => {
    const t = setup();
    for (let i = 0; i < 12; i++) expect((await t.publish("alice", pkg({ name: `p${i}` }))).status).toBe(201);
    await t.flush();
    expect(t.emits).toHaveLength(10);
  });

  it("bounds a long multi-byte source and summary so the relay frame keeps its key", async () => {
    const t = setup();
    const res = await t.publish("alice", pkg({ summary: "好".repeat(200), source: `https://github.com/o/r/tree/main/${"d".repeat(150)}` }));
    expect(res.status).toBe(201);
    await t.flush();
    const extra = t.emits[0].body.extra as Record<string, string>;
    expect(extra.summary.length).toBeLessThanOrEqual(80);
    expect(extra.source.length).toBeLessThanOrEqual(80);
    expect(new TextEncoder().encode(JSON.stringify(t.emits[0].body)).length).toBeLessThanOrEqual(600);
  });
});
