/** Dashboard sessions API — new / switch / delete with an attached switchSession callback wired in. */

import { existsSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { appendSessionMessage, patchSessionMeta, sessionPath } from "../src/memory/session.js";
import { type DashboardServerHandle, startDashboardServer } from "../src/server/index.js";

const TOKEN = "e".repeat(64);

interface FetchResult {
  status: number;
  body: any;
}

async function call(
  url: string,
  opts: { method?: string; body?: unknown } = {},
): Promise<FetchResult> {
  const u = new URL(url);
  u.searchParams.set("token", TOKEN);
  const method = opts.method ?? "GET";
  const headers: Record<string, string> = {};
  // POST / DELETE require the token in the header (CSRF defence — query alone rejected).
  if (method !== "GET") headers["X-Reasonix-Token"] = TOKEN;
  if (opts.body !== undefined) headers["Content-Type"] = "application/json";
  const res = await fetch(u.toString(), {
    method,
    headers,
    body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
  });
  const text = await res.text();
  let parsed: any = null;
  try {
    parsed = text ? JSON.parse(text) : null;
  } catch {
    parsed = text;
  }
  return { status: res.status, body: parsed };
}

describe("dashboard /sessions: new / switch / delete (attached)", () => {
  let dir: string;
  let handle: DashboardServerHandle | null = null;
  const switchCalls: Array<string | undefined> = [];
  let currentName: string | null = "alpha";

  beforeEach(async () => {
    dir = mkdtempSync(join(tmpdir(), "reasonix-sess-ops-"));
    // Re-home so listSessions reads from our temp dir, not the user's real ~/.reasonix.
    vi.stubEnv("USERPROFILE", dir);
    vi.stubEnv("HOME", dir);
    vi.spyOn(require("node:os"), "homedir").mockReturnValue(dir);

    // Seed two sessions: alpha (active) + beta.
    appendSessionMessage("alpha", { role: "user", content: "hi" });
    appendSessionMessage("beta", { role: "user", content: "hello" });
    patchSessionMeta("alpha", { workspace: dir });
    patchSessionMeta("beta", { workspace: dir });

    switchCalls.length = 0;
    currentName = "alpha";

    handle = await startDashboardServer(
      {
        mode: "attached",
        configPath: join(dir, "config.json"),
        usageLogPath: join(dir, "usage.jsonl"),
        getSessionName: () => currentName,
        switchSession: (name) => {
          switchCalls.push(name);
          currentName = name ?? "(fresh)";
          return { ok: true as const };
        },
      },
      { token: TOKEN },
    );
  });

  afterEach(async () => {
    await handle?.close();
    vi.restoreAllMocks();
    vi.unstubAllEnvs();
    if (existsSync(dir)) rmSync(dir, { recursive: true, force: true });
  });

  it("GET /api/sessions includes currentSession + canSwitch=true when wired", async () => {
    const base = handle!.url.split("?")[0]!;
    const r = await call(`${base}api/sessions`);
    expect(r.status).toBe(200);
    expect(r.body.currentSession).toBe("alpha");
    expect(r.body.canSwitch).toBe(true);
    const names = r.body.sessions.map((s: any) => s.name).sort();
    expect(names).toEqual(["alpha", "beta"]);
  });

  it("POST /api/sessions/new calls switchSession(undefined) and echoes the new name", async () => {
    const base = handle!.url.split("?")[0]!;
    const r = await call(`${base}api/sessions/new`, { method: "POST" });
    expect(r.status).toBe(200);
    expect(switchCalls).toEqual([undefined]);
    expect(r.body.name).toBe("(fresh)");
  });

  it("GET /api/sessions filters out other-workspace sessions when getCurrentCwd is wired", async () => {
    await handle?.close();
    const otherWorkspace = mkdtempSync(join(tmpdir(), "reasonix-other-ws-"));
    try {
      // alpha+beta live in `dir`; this third session belongs to a different cwd.
      appendSessionMessage("gamma", { role: "user", content: "noise" });
      patchSessionMeta("gamma", { workspace: otherWorkspace });
      // …and a subagent-style session with no workspace meta at all.
      appendSessionMessage("subagent-sub-zz-202605170235", { role: "user", content: "x" });

      handle = await startDashboardServer(
        {
          mode: "attached",
          configPath: join(dir, "config.json"),
          usageLogPath: join(dir, "usage.jsonl"),
          getSessionName: () => currentName,
          getCurrentCwd: () => dir,
          switchSession: (name) => {
            switchCalls.push(name);
            currentName = name ?? "(fresh)";
            return { ok: true as const };
          },
        },
        { token: TOKEN },
      );

      const base = handle!.url.split("?")[0]!;
      const r = await call(`${base}api/sessions`);
      expect(r.status).toBe(200);
      const names = r.body.sessions.map((s: any) => s.name).sort();
      // gamma (other workspace) + subagent (no meta) MUST be filtered out.
      expect(names).toEqual(["alpha", "beta"]);
    } finally {
      rmSync(otherWorkspace, { recursive: true, force: true });
    }
  });

  it("POST /api/sessions/<name>/switch calls switchSession(name)", async () => {
    const base = handle!.url.split("?")[0]!;
    const r = await call(`${base}api/sessions/beta/switch`, { method: "POST" });
    expect(r.status).toBe(200);
    expect(switchCalls).toEqual(["beta"]);
  });

  it("POST /api/sessions/<missing>/switch returns 404 without calling switchSession", async () => {
    const base = handle!.url.split("?")[0]!;
    const r = await call(`${base}api/sessions/ghost/switch`, { method: "POST" });
    expect(r.status).toBe(404);
    expect(switchCalls).toEqual([]);
  });

  it("DELETE /api/sessions/<active> returns 409 and leaves the file intact", async () => {
    const base = handle!.url.split("?")[0]!;
    const r = await call(`${base}api/sessions/alpha`, { method: "DELETE" });
    expect(r.status).toBe(409);
    expect(existsSync(sessionPath("alpha"))).toBe(true);
  });

  it("DELETE /api/sessions/<non-active> unlinks the jsonl", async () => {
    const base = handle!.url.split("?")[0]!;
    expect(existsSync(sessionPath("beta"))).toBe(true);
    const r = await call(`${base}api/sessions/beta`, { method: "DELETE" });
    expect(r.status).toBe(200);
    expect(r.body.deleted).toBe("beta");
    expect(existsSync(sessionPath("beta"))).toBe(false);
  });
});
