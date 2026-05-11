/** `startDashboardServer({ port })` — pinned port binds and rejects EADDRINUSE. */

import { existsSync, mkdtempSync, rmSync } from "node:fs";
import { createServer } from "node:http";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { type DashboardServerHandle, startDashboardServer } from "../src/server/index.js";

const TOKEN = "test-token";

function ctx(dir: string) {
  return {
    mode: "standalone" as const,
    configPath: join(dir, "config.json"),
    usageLogPath: join(dir, "usage.jsonl"),
  };
}

/** Pick a likely-free port by binding a throwaway server with port 0 and reading what the OS gave us. */
async function reserveEphemeralPort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const probe = createServer();
    probe.once("error", reject);
    probe.listen(0, "127.0.0.1", () => {
      const addr = probe.address();
      if (!addr || typeof addr === "string") {
        probe.close();
        reject(new Error("no address"));
        return;
      }
      const port = addr.port;
      probe.close(() => resolve(port));
    });
  });
}

describe("startDashboardServer port pinning", () => {
  let dir: string;
  let handle: DashboardServerHandle | undefined;

  beforeEach(() => {
    dir = mkdtempSync(join(tmpdir(), "reasonix-dashport-"));
  });

  afterEach(async () => {
    await handle?.close();
    handle = undefined;
    if (existsSync(dir)) rmSync(dir, { recursive: true, force: true });
  });

  it("binds to the requested port when opts.port is set", async () => {
    const port = await reserveEphemeralPort();
    handle = await startDashboardServer(ctx(dir), { token: TOKEN, port });
    expect(handle.port).toBe(port);
    expect(handle.url).toContain(`:${port}/`);
  });

  it("defaults to an ephemeral port when opts.port is absent", async () => {
    handle = await startDashboardServer(ctx(dir), { token: TOKEN });
    expect(handle.port).toBeGreaterThan(0);
    expect(handle.port).toBeLessThan(65536);
  });

  it("rejects when the requested port is already bound (EADDRINUSE)", async () => {
    const port = await reserveEphemeralPort();
    handle = await startDashboardServer(ctx(dir), { token: TOKEN, port });

    await expect(startDashboardServer(ctx(dir), { token: TOKEN, port })).rejects.toThrow(
      /EADDRINUSE/,
    );
  });
});
