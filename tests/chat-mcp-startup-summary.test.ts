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
  readConfig?: { mcpDisabled?: string[]; setupCompleted?: boolean; mcp?: string[] };
  initializeError?: Error;
  bridgeError?: Error;
  mcp?: string[];
  lang?: "EN" | "zh-CN";
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

  const [{ chatCommand }, { ToolRegistry }, { setLanguageRuntime }] = await Promise.all([
    import("../src/cli/commands/chat.js"),
    import("../src/tools.js"),
    import("../src/i18n/index.js"),
  ]);
  setLanguageRuntime(opts?.lang ?? "EN");

  await chatCommand({
    model: "deepseek-chat",
    system: "s",
    mcp: opts?.mcp ?? ["fs=npx -y @scope/fs /tmp"],
    seedTools: new ToolRegistry(),
  });

  expect(capturedProps).not.toBeNull();
  return capturedProps as {
    mcpServers: Array<{ label: string; spec: string }>;
    mcpSpecs: string[];
    startupInfoHints: string[];
  };
}

// Dynamic chat.js / tools.js import inside captureStartupState pushes
// past the 5s default under full-suite worker contention; pass in
// isolation. 15s leaves headroom for cold module-cache + slow CI hosts
// without making the suite noticeably slower in the happy path.
describe("chatCommand MCP startup summary states", { timeout: 15_000 }, () => {
  let savedRenderer: string | undefined;
  beforeEach(() => {
    vi.spyOn(process.stderr, "write").mockImplementation(() => true);
    savedRenderer = process.env.REASONIX_RENDERER;
    // Renderer takes over the TTY + holds a singleton; tests probe chat
    // startup state only, no real renderer needed.
    process.env.REASONIX_RENDERER = "node";
  });

  afterEach(() => {
    vi.restoreAllMocks();
    if (savedRenderer === undefined) process.env.REASONIX_RENDERER = undefined;
    else process.env.REASONIX_RENDERER = savedRenderer;
  });

  it("passes mcpSpecs through with empty initial mcpServers — bridging is deferred to App mount", async () => {
    const props = await captureStartupState();

    expect(props.mcpSpecs).toEqual(["fs=npx -y @scope/fs /tmp"]);
    expect(props.mcpServers).toEqual([]);
    expect(mocks.bridgeMcpToolsMock).not.toHaveBeenCalled();
  });

  it("preserves disabled startup specs for marketplace fallback — App.tsx skips them on mount", async () => {
    const props = await captureStartupState({
      readConfig: { mcpDisabled: ["fs"] },
    });

    expect(props.mcpSpecs).toEqual(["fs=npx -y @scope/fs /tmp"]);
    expect(props.mcpServers).toEqual([]);
    expect(mocks.bridgeMcpToolsMock).not.toHaveBeenCalled();
  });

  it("never blocks chatCommand on bridge failure — App.tsx surfaces the lifecycle error post-mount", async () => {
    const props = await captureStartupState({
      initializeError: new Error("spawn failed"),
    });

    expect(props.mcpSpecs).toEqual(["fs=npx -y @scope/fs /tmp"]);
    expect(props.mcpServers).toEqual([]);
    expect(mocks.initializeMock).not.toHaveBeenCalled();
  });

  it("adds empty-MCP hint exactly when setup is completed and configured MCP list is empty", async () => {
    const props = await captureStartupState({
      readConfig: { setupCompleted: true, mcp: [] },
      mcp: [],
    });

    expect(props.startupInfoHints).toEqual([
      "\u2139 no MCP servers configured \u2014 try: `reasonix setup` to re-pick, or `reasonix mcp install filesystem`",
    ]);
  });

  it("does not add empty-MCP hint when configured MCP list is non-empty", async () => {
    const props = await captureStartupState({
      readConfig: { setupCompleted: true, mcp: ["fs=npx -y @scope/fs /tmp"] },
      mcp: ["fs=npx -y @scope/fs /tmp"],
    });

    expect(props.startupInfoHints).toEqual([]);
  });

  it("renders empty-MCP hint in zh-CN locale", async () => {
    const props = await captureStartupState({
      readConfig: { setupCompleted: true, mcp: [] },
      mcp: [],
      lang: "zh-CN",
    });
    expect(props.startupInfoHints).toEqual([
      "\u2139 未配置 MCP 服务器 —— 可尝试：`reasonix setup` 重新选择，或 `reasonix mcp install filesystem`",
    ]);
  });
});
