/** `mcpEnvFor` lookup + `buildTransportFromSpec` transport selection — issue #376 plumbing. */

import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { type ReasonixConfig, mcpEnvFor } from "../src/config.js";
import { parseMcpSpec } from "../src/mcp/spec.js";
import { SseTransport } from "../src/mcp/sse.js";
import { StdioTransport } from "../src/mcp/stdio.js";
import { StreamableHttpTransport } from "../src/mcp/streamable-http.js";
import { buildTransportFromSpec } from "../src/mcp/transport-from-spec.js";

describe("mcpEnvFor", () => {
  it("returns the env map for a configured server name", () => {
    const cfg: ReasonixConfig = {
      mcpEnv: { github: { GITHUB_PERSONAL_ACCESS_TOKEN: "ghp_abc" } },
    };
    expect(mcpEnvFor("github", cfg)).toEqual({ GITHUB_PERSONAL_ACCESS_TOKEN: "ghp_abc" });
  });

  it("returns undefined when the server name is null (anonymous spec)", () => {
    const cfg: ReasonixConfig = {
      mcpEnv: { github: { GITHUB_PERSONAL_ACCESS_TOKEN: "ghp_abc" } },
    };
    expect(mcpEnvFor(null, cfg)).toBeUndefined();
    expect(mcpEnvFor(undefined, cfg)).toBeUndefined();
  });

  it("returns undefined when no entry exists for the name", () => {
    const cfg: ReasonixConfig = {
      mcpEnv: { github: { GITHUB_PERSONAL_ACCESS_TOKEN: "ghp_abc" } },
    };
    expect(mcpEnvFor("filesystem", cfg)).toBeUndefined();
  });

  it("returns undefined when the config has no mcpEnv at all", () => {
    expect(mcpEnvFor("github", {})).toBeUndefined();
  });

  it("drops empty-string values so blanks in config don't blank out process.env", () => {
    const cfg: ReasonixConfig = {
      mcpEnv: { github: { TOKEN: "real", LEFT_EMPTY: "" } },
    };
    expect(mcpEnvFor("github", cfg)).toEqual({ TOKEN: "real" });
  });

  it("returns undefined when every value is empty (no real overlay to apply)", () => {
    const cfg: ReasonixConfig = { mcpEnv: { github: { A: "", B: "" } } };
    expect(mcpEnvFor("github", cfg)).toBeUndefined();
  });
});

describe("buildTransportFromSpec", () => {
  it("returns an SseTransport for `http://…` specs", () => {
    const spec = parseMcpSpec("svc=http://localhost:1234/sse");
    expect(buildTransportFromSpec(spec)).toBeInstanceOf(SseTransport);
  });

  it("returns a StreamableHttpTransport for `streamable+http://…` specs", () => {
    const spec = parseMcpSpec("svc=streamable+http://localhost:1234/mcp");
    expect(buildTransportFromSpec(spec)).toBeInstanceOf(StreamableHttpTransport);
  });

  it("returns a StdioTransport for a bare command spec", () => {
    const spec = parseMcpSpec("svc=node -v");
    const transport = buildTransportFromSpec(spec, { env: { FOO: "bar" } });
    expect(transport).toBeInstanceOf(StdioTransport);
    void transport.close();
  });

  it("propagates the env overlay to the spawned child (end-to-end)", async () => {
    const dir = mkdtempSync(join(tmpdir(), "reasonix-mcp-env-"));
    const scriptPath = join(dir, "emit-env.cjs");
    writeFileSync(
      scriptPath,
      'process.stdout.write(JSON.stringify({jsonrpc:"2.0",method:"x",params:{token:process.env.REASONIX_MCP_TEST_TOKEN||""}})+"\\n");',
      "utf8",
    );
    const transport = new StdioTransport({
      command: process.execPath,
      args: [scriptPath],
      env: { REASONIX_MCP_TEST_TOKEN: "from-config-overlay" },
      shell: false,
    });
    try {
      const iter = transport.messages();
      const next = await iter.next();
      expect(next.done).toBe(false);
      const msg = next.value as { params?: { token?: string } };
      expect(msg.params?.token).toBe("from-config-overlay");
    } finally {
      await transport.close();
      rmSync(dir, { recursive: true, force: true });
    }
  }, 10_000);
});
