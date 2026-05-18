/** Child stderr is piped, not inherited — only forwarded under REASONIX_DEBUG_MCP=1. */

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { StdioTransport } from "../src/mcp/stdio.js";

const STDERR_THEN_EXIT =
  "process.stderr.write('INFO server.py:534 Processing request of type ListPromptsRequest\\n'); process.exit(0)";

async function awaitChildExit(t: StdioTransport): Promise<void> {
  for await (const _msg of t.messages()) {
    // The script never emits JSON-RPC, so the body never runs.
  }
}

describe("StdioTransport child stderr handling", { timeout: 5_000 }, () => {
  let writeSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    writeSpy = vi.spyOn(process.stderr, "write").mockImplementation(() => true);
  });

  afterEach(() => {
    writeSpy.mockRestore();
    vi.unstubAllEnvs();
  });

  it("does not forward child stderr to our stderr when REASONIX_DEBUG_MCP is unset", async () => {
    vi.stubEnv("REASONIX_DEBUG_MCP", "");
    const t = new StdioTransport({
      command: "node",
      args: ["-e", STDERR_THEN_EXIT],
      shell: false,
    });
    await awaitChildExit(t);
    await t.close();

    const stderrCalls = writeSpy.mock.calls.map((c) => String(c[0]));
    expect(stderrCalls.some((s) => s.includes("server.py:534"))).toBe(false);
  });

  it("forwards child stderr to our stderr when REASONIX_DEBUG_MCP=1", async () => {
    vi.stubEnv("REASONIX_DEBUG_MCP", "1");
    const t = new StdioTransport({
      command: "node",
      args: ["-e", STDERR_THEN_EXIT],
      shell: false,
    });
    await awaitChildExit(t);
    await t.close();

    const stderrCalls = writeSpy.mock.calls.map((c) => String(c[0]));
    expect(stderrCalls.some((s) => s.includes("server.py:534"))).toBe(true);
  });
});
