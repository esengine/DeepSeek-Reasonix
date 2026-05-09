import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => {
  const renderMock = vi.fn();
  const loadApiKeyMock = vi.fn(() => "sk-test");
  const readConfigMock = vi.fn(() => ({ mcpDisabled: [] as string[] }));
  const searchEnabledMock = vi.fn(() => false);
  const loadDotenvMock = vi.fn();
  const resolveSessionMock = vi.fn(() => ({ resolved: "session-1" }));
  const listSessionsForWorkspaceMock = vi.fn(() => [] as string[]);
  const initializeMock = vi.fn(async () => undefined);
  const closeMock = vi.fn(async () => undefined);
  const bridgeMcpToolsMock = vi.fn(async (_client: unknown, opts: any) => ({
    registeredNames: [],
    env: {
      registry: opts.registry,
      host: opts.host,
      prefix: opts.namePrefix ?? "",
      maxResultChars: 32_000,
      tracker: null,
    },
  }));
  const inspectMcpServerMock = vi.fn(async () => ({
    protocolVersion: "2024-11-05",
    serverInfo: { name: "fs-server", version: "1.0.0" },
    capabilities: { tools: {} },
    tools: { supported: true as const, items: [] },
    resources: { supported: false as const, reason: "method not found" },
    prompts: { supported: false as const, reason: "method not found" },
    elapsedMs: 42,
  }));
  const parseMcpSpecMock = vi.fn((raw: string) => ({
    name: raw.split("=")[0] ?? "anon",
    transport: "stdio" as const,
    command: "mock-mcp",
    args: [],
  }));

  class FakeMcpClient {
    protocolVersion = "2024-11-05";
    serverInfo = { name: "fs-server", version: "1.0.0" };
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
    bridgeMcpToolsMock,
    closeMock,
    FakeMcpClient,
    FakeTransport,
    initializeMock,
    inspectMcpServerMock,
    listSessionsForWorkspaceMock,
    loadApiKeyMock,
    loadDotenvMock,
    parseMcpSpecMock,
    readConfigMock,
    renderMock,
    resolveSessionMock,
    searchEnabledMock,
  };
});

vi.mock("ink", () => ({
  render: mocks.renderMock,
}));

vi.mock("../src/config.js", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../src/config.js")>();
  return {
    ...actual,
    loadApiKey: mocks.loadApiKeyMock,
    readConfig: mocks.readConfigMock,
    searchEnabled: mocks.searchEnabledMock,
  };
});

vi.mock("../src/env.js", () => ({
  loadDotenv: mocks.loadDotenvMock,
}));

vi.mock("../src/memory/session.js", () => ({
  deleteSession: vi.fn(),
  listSessionsForWorkspace: mocks.listSessionsForWorkspaceMock,
  renameSession: vi.fn(),
  resolveSession: mocks.resolveSessionMock,
}));

vi.mock("../src/mcp/client.js", () => ({
  McpClient: mocks.FakeMcpClient,
}));

vi.mock("../src/mcp/inspect.js", () => ({
  inspectMcpServer: mocks.inspectMcpServerMock,
}));

vi.mock("../src/mcp/registry.js", () => ({
  bridgeMcpTools: mocks.bridgeMcpToolsMock,
}));

vi.mock("../src/mcp/spec.js", () => ({
  parseMcpSpec: mocks.parseMcpSpecMock,
}));

vi.mock("../src/mcp/sse.js", () => ({
  SseTransport: mocks.FakeTransport,
}));

vi.mock("../src/mcp/stdio.js", () => ({
  StdioTransport: mocks.FakeTransport,
}));

vi.mock("../src/mcp/streamable-http.js", () => ({
  StreamableHttpTransport: mocks.FakeTransport,
}));

async function captureStartupState(opts?: {
  readConfig?: { mcpDisabled?: string[] };
  initializeError?: Error;
  bridgeError?: Error;
}) {
  vi.resetModules();
  mocks.renderMock.mockReset();
  mocks.loadDotenvMock.mockClear();
  mocks.loadApiKeyMock.mockClear();
  mocks.initializeMock.mockReset();
  mocks.closeMock.mockReset();
  mocks.bridgeMcpToolsMock.mockReset();
  mocks.inspectMcpServerMock.mockReset();
  mocks.parseMcpSpecMock.mockReset();
  mocks.readConfigMock.mockReset();
  mocks.listSessionsForWorkspaceMock.mockReset();
  mocks.resolveSessionMock.mockReset();
  mocks.searchEnabledMock.mockReset();

  mocks.readConfigMock.mockReturnValue(opts?.readConfig ?? { mcpDisabled: [] });
  mocks.searchEnabledMock.mockReturnValue(false);
  mocks.listSessionsForWorkspaceMock.mockReturnValue([]);
  mocks.resolveSessionMock.mockReturnValue({ resolved: "session-1" });
  mocks.parseMcpSpecMock.mockImplementation((raw: string) => ({
    name: raw.split("=")[0] ?? "anon",
    transport: "stdio" as const,
    command: "mock-mcp",
    args: [],
  }));
  mocks.initializeMock.mockImplementation(async () => {
    if (opts?.initializeError) throw opts.initializeError;
  });
  mocks.bridgeMcpToolsMock.mockImplementation(async (_client: unknown, bridgeOpts: any) => {
    if (opts?.bridgeError) throw opts.bridgeError;
    return {
      registeredNames: [],
      env: {
        registry: bridgeOpts.registry,
        host: bridgeOpts.host,
        prefix: bridgeOpts.namePrefix ?? "",
        maxResultChars: 32_000,
        tracker: null,
      },
    };
  });
  mocks.inspectMcpServerMock.mockImplementation(async () => ({
    protocolVersion: "2024-11-05",
    serverInfo: { name: "fs-server", version: "1.0.0" },
    capabilities: { tools: {} },
    tools: { supported: true as const, items: [] },
    resources: { supported: false as const, reason: "method not found" },
    prompts: { supported: false as const, reason: "method not found" },
    elapsedMs: 42,
  }));

  let capturedProps: Record<string, unknown> | null = null;
  mocks.renderMock.mockImplementation((element: { props: Record<string, unknown> }) => {
    capturedProps = element.props;
    return { waitUntilExit: async () => undefined };
  });

  const [{ chatCommand }, { ToolRegistry }] = await Promise.all([
    import("../src/cli/commands/chat.js"),
    import("../src/tools.js"),
  ]);

  await chatCommand({
    model: "deepseek-chat",
    system: "s",
    mcp: ["fs=npx -y @scope/fs /tmp"],
    seedTools: new ToolRegistry(),
  });

  expect(capturedProps).not.toBeNull();
  return capturedProps as {
    mcpServers: Array<{ label: string; spec: string }>;
    mcpSpecs: string[];
  };
}

// Dynamic chat.js / tools.js import inside captureStartupState pushes
// past the 5s default under full-suite worker contention; pass in
// isolation. 15s leaves headroom for cold module-cache + slow CI hosts
// without making the suite noticeably slower in the happy path.
describe("chatCommand MCP startup summary states", { timeout: 15_000 }, () => {
  beforeEach(() => {
    vi.spyOn(process.stderr, "write").mockImplementation(() => true);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("passes live bridged servers into the initial MCP props", async () => {
    const props = await captureStartupState();

    expect(props.mcpSpecs).toEqual(["fs=npx -y @scope/fs /tmp"]);
    expect(props.mcpServers).toHaveLength(1);
    expect(props.mcpServers[0]).toMatchObject({
      label: "fs",
      spec: "fs=npx -y @scope/fs /tmp",
    });
  });

  it("preserves disabled startup specs for marketplace fallback even with no live servers", async () => {
    const props = await captureStartupState({
      readConfig: { mcpDisabled: ["fs"] },
    });

    expect(props.mcpSpecs).toEqual(["fs=npx -y @scope/fs /tmp"]);
    expect(props.mcpServers).toEqual([]);
    expect(mocks.bridgeMcpToolsMock).not.toHaveBeenCalled();
  });

  it("preserves unbridged startup specs when startup fails before a live summary exists", async () => {
    const props = await captureStartupState({
      initializeError: new Error("spawn failed"),
    });

    expect(props.mcpSpecs).toEqual(["fs=npx -y @scope/fs /tmp"]);
    expect(props.mcpServers).toEqual([]);
  });
});
