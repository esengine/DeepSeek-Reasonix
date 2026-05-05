import { existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  type DashboardServerHandle,
  startDashboardServer,
} from "../src/server/index.js";

const TOKEN = "f".repeat(64);

async function api(
  base: string,
  path: string,
  opts: { method?: string; body?: unknown } = {},
): Promise<{ status: number; body: unknown }> {
  const url = new URL(`${base}${path}`);
  const method = opts.method ?? "GET";
  const headers: Record<string, string> = {};
  if (method === "GET") {
    url.searchParams.set("token", TOKEN);
  } else {
    headers["X-Reasonix-Token"] = TOKEN;
  }
  if (opts.body !== undefined) headers["Content-Type"] = "application/json";
  const res = await fetch(url.toString(), {
    method,
    headers,
    body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
  });
  const text = await res.text();
  let parsed: unknown = null;
  try {
    parsed = text ? JSON.parse(text) : null;
  } catch {
    parsed = text;
  }
  return { status: res.status, body: parsed };
}

function readConfigFile(path: string): Record<string, unknown> {
  try {
    return JSON.parse(readFileSync(path, "utf8")) as Record<string, unknown>;
  } catch {
    return {};
  }
}

describe("settings API: combined POST persistence", () => {
  let dir: string;
  let cfgPath: string;
  let usagePath: string;
  let handle: DashboardServerHandle | null = null;

  beforeEach(async () => {
    dir = mkdtempSync(join(tmpdir(), "reasonix-settings-"));
    cfgPath = join(dir, "config.json");
    usagePath = join(dir, "usage.jsonl");
    handle = await startDashboardServer(
      {
        mode: "standalone",
        configPath: cfgPath,
        usageLogPath: usagePath,
      },
      { token: TOKEN },
    );
  });

  afterEach(async () => {
    await handle?.close();
    if (existsSync(dir)) rmSync(dir, { recursive: true, force: true });
  });

  function baseUrl(): string {
    return handle!.url.split("?")[0]!;
  }

  function post(body: unknown) {
    const u = new URL(`${baseUrl()}api/settings`);
    const headers: Record<string, string> = {
      "X-Reasonix-Token": TOKEN,
      "Content-Type": "application/json",
    };
    return fetch(u.toString(), {
      method: "POST",
      headers,
      body: JSON.stringify(body),
    }).then(async (r) => ({ status: r.status, body: await r.json().catch(() => null) as unknown }));
  }

  function get() {
    const u = new URL(`${baseUrl()}api/settings`);
    u.searchParams.set("token", TOKEN);
    return fetch(u.toString()).then(async (r) => ({
      status: r.status,
      body: (await r.json().catch(() => null)) as unknown,
    }));
  }

  it("lang-only POST persists language", async () => {
    const r = await post({ lang: "zh-CN" });
    expect(r.status).toBe(200);
    expect((r.body as Record<string, unknown>).changed).toContain("lang");
    expect(readConfigFile(cfgPath).lang).toBe("zh-CN");
  });

  it("lang + baseUrl persists both fields", async () => {
    const r = await post({ lang: "EN", baseUrl: "https://new.example.com" });
    expect(r.status).toBe(200);
    const cfg = readConfigFile(cfgPath);
    expect(cfg.lang).toBe("EN");
    expect(cfg.baseUrl).toBe("https://new.example.com");
  });

  it("lang + preset persists both fields", async () => {
    const r = await post({ lang: "zh-CN", preset: "pro" });
    expect(r.status).toBe(200);
    const cfg = readConfigFile(cfgPath);
    expect(cfg.lang).toBe("zh-CN");
    expect(cfg.preset).toBe("pro");
  });

  it("lang + search persists both fields", async () => {
    const r = await post({ lang: "EN", search: false });
    expect(r.status).toBe(200);
    const cfg = readConfigFile(cfgPath);
    expect(cfg.lang).toBe("EN");
    expect(cfg.search).toBe(false);
  });

  it("lang + apiKey persists both fields", async () => {
    const r = await post({ lang: "EN", apiKey: "sk-deadbeef1234567890abcdef" });
    expect(r.status).toBe(200);
    const cfg = readConfigFile(cfgPath);
    expect(cfg.lang).toBe("EN");
    expect(cfg.apiKey).toBe("sk-deadbeef1234567890abcdef");
  });

  it("setting baseUrl does not drop existing lang", async () => {
    writeFileSync(cfgPath, JSON.stringify({ lang: "zh-CN" }), "utf8");
    await handle!.close();
    handle = await startDashboardServer(
      { mode: "standalone", configPath: cfgPath, usageLogPath: usagePath },
      { token: TOKEN },
    );
    const r = await post({ baseUrl: "https://new.example.com" });
    expect(r.status).toBe(200);
    const cfg = readConfigFile(cfgPath);
    expect(cfg.lang).toBe("zh-CN");
    expect(cfg.baseUrl).toBe("https://new.example.com");
  });

  it("rejects invalid lang", async () => {
    const r = await post({ lang: "de" });
    expect(r.status).toBe(400);
    expect((r.body as Record<string, unknown>).error).toContain("lang must be one of");
  });

  it("rejects invalid preset", async () => {
    const r = await post({ preset: "turbo" });
    expect(r.status).toBe(400);
    expect((r.body as Record<string, unknown>).error).toContain("preset must be");
  });

  it("rejects non-boolean search", async () => {
    const r = await post({ search: "yes" });
    expect(r.status).toBe(400);
    expect((r.body as Record<string, unknown>).error).toContain("search must be a boolean");
  });
});
