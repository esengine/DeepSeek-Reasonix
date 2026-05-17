/** `startDashboardServer({ host, token })` — LAN exposure + stable token (#968). */

import { existsSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { type DashboardServerHandle, startDashboardServer } from "../src/server/index.js";

const TOKEN = "stable-pinned-token-1234567890";

function ctx(dir: string) {
  return {
    mode: "standalone" as const,
    configPath: join(dir, "config.json"),
    usageLogPath: join(dir, "usage.jsonl"),
  };
}

describe("startDashboardServer host + token (#968)", () => {
  let dir: string;
  let handle: DashboardServerHandle | undefined;
  let writeSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    dir = mkdtempSync(join(tmpdir(), "reasonix-dashhost-"));
    writeSpy = vi.spyOn(process.stderr, "write").mockImplementation(() => true);
  });

  afterEach(async () => {
    await handle?.close();
    handle = undefined;
    writeSpy.mockRestore();
    if (existsSync(dir)) rmSync(dir, { recursive: true, force: true });
  });

  it("defaults to 127.0.0.1 when no host is given and emits no LAN warning", async () => {
    handle = await startDashboardServer(ctx(dir), { token: TOKEN });
    expect(handle.url).toMatch(/^http:\/\/127\.0\.0\.1:\d+\/\?token=/);
    const warnings = writeSpy.mock.calls.map((c) => String(c[0])).filter((s) => s.includes("▲"));
    expect(warnings).toEqual([]);
  });

  it("reuses opts.token verbatim instead of minting a fresh one", async () => {
    handle = await startDashboardServer(ctx(dir), { token: TOKEN });
    expect(handle.token).toBe(TOKEN);
    expect(handle.url).toContain(`token=${TOKEN}`);
  });

  it("binds 0.0.0.0 when requested and prints a stderr warning", async () => {
    handle = await startDashboardServer(ctx(dir), { token: TOKEN, host: "0.0.0.0" });
    expect(handle.url).toMatch(/^http:\/\/0\.0\.0\.0:\d+\/\?token=/);
    const warnings = writeSpy.mock.calls.map((c) => String(c[0])).filter((s) => s.includes("▲"));
    expect(warnings.length).toBe(1);
    expect(warnings[0]).toContain("non-loopback");
    expect(warnings[0]).toContain("token");
  });

  it("does not warn for ::1 or localhost (still loopback)", async () => {
    handle = await startDashboardServer(ctx(dir), { token: TOKEN, host: "localhost" });
    const warnings = writeSpy.mock.calls.map((c) => String(c[0])).filter((s) => s.includes("▲"));
    expect(warnings).toEqual([]);
  });
});
