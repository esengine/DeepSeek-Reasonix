/** StdioTransport.close() must swallow child.kill() errors (e.g. EINVAL on Windows). */

import type { ChildProcess } from "node:child_process";
import { describe, expect, it } from "vitest";
import { StdioTransport } from "../src/mcp/stdio.js";

describe("StdioTransport.close()", () => {
  it("swallows kill() EINVAL without throwing", async () => {
    const t = new StdioTransport({
      command: "node",
      args: ["-e", "process.exit(0)"],
      shell: false,
    });
    // Let child exit so .kill() hits a reaped/zombie-like state.
    await new Promise((r) => setTimeout(r, 200));
    await expect(t.close()).resolves.toBeUndefined();
  });

  it("does not throw on failed spawn", async () => {
    const t = new StdioTransport({
      command: "nonexistent_command_that_does_not_exist_12345",
      shell: false,
    });
    const iter = t.messages();
    const msg = await iter[Symbol.asyncIterator]().next();
    expect(msg.value?.error?.code).toBe(-32000);
    await expect(t.close()).resolves.toBeUndefined();
  });

  it("is idempotent — second close() is a no-op", async () => {
    const t = new StdioTransport({
      command: "node",
      args: ["-e", "setTimeout(() => {}, 5000)"],
      shell: false,
    });
    await t.close();
    await expect(t.close()).resolves.toBeUndefined();
  });

  it("swallows kill() EINVAL via monkey-patch", async () => {
    const t = new StdioTransport({
      command: "node",
      args: ["-e", "setTimeout(() => {}, 5000)"],
      shell: false,
    });
    const child = (t as unknown as { child: ChildProcess }).child;
    const originalKill = child.kill.bind(child);
    let killCalled = false;
    // Force EINVAL to verify the catch path works.
    child.kill = (signal?: NodeJS.Signals | number) => {
      killCalled = true;
      if (signal === "SIGTERM") {
        const err = new Error("kill EINVAL");
        (err as NodeJS.ErrnoException).code = "EINVAL";
        throw err;
      }
      return originalKill(signal);
    };
    await expect(t.close()).resolves.toBeUndefined();
    expect(killCalled).toBe(true);
    try {
      if (!child.killed) child.kill();
    } catch {
      /* already dead */
    }
  });
});
