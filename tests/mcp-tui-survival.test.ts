import { describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => {
  const initializeMock = vi.fn(async () => undefined);
  const closeMock = vi.fn(async () => undefined);
  const bridgeMcpToolsMock = vi.fn(async (_client: unknown, opts: any) => {
    // Simulate what real bridgeMcpTools does — register tools into the registry.
    opts.registry.register({ name: "mcp_fs_read", fn: () => "ok", description: "read" });
    opts.registry.register({ name: "mcp_fs_write", fn: () => "ok", description: "write" });
    return {
      registeredNames: ["mcp_fs_read", "mcp_fs_write"],
      env: {
        registry: opts.registry,
        host: opts.host,
        prefix: opts.namePrefix ?? "",
        maxResultChars: 32_000,
        tracker: null,
      },
    };
  });
  const inspectMcpServerMock = vi.fn(async () => ({
    protocolVersion: "2024-11-05",
    serverInfo: { name: "fake", version: "1.0.0" },
    capabilities: { tools: {} },
    tools: { supported: true as const, items: [] },
    resources: { supported: false as const, reason: "method not found" },
    prompts: { supported: false as const, reason: "method not found" },
    elapsedMs: 1,
  }));
  const readConfigMock = vi.fn(() => ({ mcpDisabled: [] as string[] }));

  class FakeMcpClient {
    protocolVersion = "2024-11-05";
    serverInfo = { name: "fake", version: "1.0.0" };
    serverCapabilities = { tools: {} };
    async initialize() {
      return initializeMock();
    }
    async close() {
      return closeMock();
    }
  }

  class FakeTransport {}

  return {
    initializeMock,
    closeMock,
    bridgeMcpToolsMock,
    inspectMcpServerMock,
    readConfigMock,
    FakeMcpClient,
    FakeTransport,
  };
});

vi.mock("../src/config.js", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../src/config.js")>();
  return { ...actual, readConfig: mocks.readConfigMock };
});

vi.mock("../src/mcp/client.js", () => ({ McpClient: mocks.FakeMcpClient }));
vi.mock("../src/mcp/inspect.js", () => ({ inspectMcpServer: mocks.inspectMcpServerMock }));
vi.mock("../src/mcp/registry.js", () => ({ bridgeMcpTools: mocks.bridgeMcpToolsMock }));
vi.mock("../src/mcp/sse.js", () => ({ SseTransport: mocks.FakeTransport }));
vi.mock("../src/mcp/stdio.js", () => ({ StdioTransport: mocks.FakeTransport }));
vi.mock("../src/mcp/streamable-http.js", () => ({
  StreamableHttpTransport: mocks.FakeTransport,
}));

describe("MCP tools survive TUI lifecycle events", () => {
  it("ImmutablePrefix created from a registry with MCP tools includes them", async () => {
    const { ToolRegistry } = await import("../src/tools.js");
    const { ImmutablePrefix } = await import("../src/memory/runtime.js");

    const tools = new ToolRegistry();
    tools.register({ name: "native_tool", fn: () => "ok" });
    tools.register({ name: "mcp_fs_read", fn: () => "ok" });

    const prefix = new ImmutablePrefix({
      system: "test",
      toolSpecs: tools.specs(),
    });

    const names = prefix.toolSpecs.map((s) => s.function.name);
    expect(names).toContain("native_tool");
    expect(names).toContain("mcp_fs_read");
  });

  it("CacheFirstLoop.clearLog() preserves prefix toolSpecs", async () => {
    const { ToolRegistry } = await import("../src/tools.js");
    const { ImmutablePrefix } = await import("../src/memory/runtime.js");
    const { CacheFirstLoop } = await import("../src/loop.js");
    const { DeepSeekClient } = await import("../src/client.js");

    const tools = new ToolRegistry();
    tools.register({ name: "mcp_tool", fn: () => "ok" });

    const prefix = new ImmutablePrefix({
      system: "test",
      toolSpecs: tools.specs(),
    });

    const loop = new CacheFirstLoop({
      client: new DeepSeekClient({ apiKey: "sk-test" }),
      prefix,
      tools,
      model: "deepseek-chat",
    });

    loop.clearLog();

    const names = loop.prefix.toolSpecs.map((s) => s.function.name);
    expect(names).toContain("mcp_tool");
  });

  it("re-bridging an existing spec onto a new loop preserves tools via registry.specs()", async () => {
    mocks.readConfigMock.mockReturnValue({ mcpDisabled: [] });
    mocks.initializeMock.mockImplementation(async () => undefined);

    const [
      { createMcpRuntime },
      { ToolRegistry },
      { ImmutablePrefix },
      { CacheFirstLoop },
      { DeepSeekClient },
    ] = await Promise.all([
      import("../src/cli/commands/mcp-runtime.js"),
      import("../src/tools.js"),
      import("../src/memory/runtime.js"),
      import("../src/loop.js"),
      import("../src/client.js"),
    ]);

    const tools = new ToolRegistry();
    const runtime = createMcpRuntime({
      getTools: () => tools,
      getMcpPrefix: () => undefined,
      getRequestedCount: () => 1,
      progressSink: { current: null },
    });

    // First bridge — simulates initial App mount
    const spec = "fs=npx -y @scope/fs /tmp";
    const firstLoop = new CacheFirstLoop({
      client: new DeepSeekClient({ apiKey: "sk-test" }),
      prefix: new ImmutablePrefix({ system: "test", toolSpecs: tools.specs() }),
      tools,
      model: "deepseek-chat",
    });

    const firstResult = await runtime.addSpec(spec, firstLoop);
    expect(firstResult.ok).toBe(true);
    expect(tools.has("mcp_fs_read")).toBe(true);
    expect(tools.has("mcp_fs_write")).toBe(true);

    // Second bridge — simulates App remount on session switch.
    // The registry already has the tools; the new prefix gets them from tools.specs().
    const secondLoop = new CacheFirstLoop({
      client: new DeepSeekClient({ apiKey: "sk-test" }),
      prefix: new ImmutablePrefix({ system: "test", toolSpecs: tools.specs() }),
      tools,
      model: "deepseek-chat",
    });

    const secondResult = await runtime.addSpec(spec, secondLoop);
    expect(secondResult.ok).toBe(true);

    // Even though addSpec returned early (records.has), the new prefix already
    // has the tools because it was built from tools.specs().
    const names = secondLoop.prefix.toolSpecs.map((s) => s.function.name);
    expect(names).toContain("mcp_fs_read");
    expect(names).toContain("mcp_fs_write");
  });
});
