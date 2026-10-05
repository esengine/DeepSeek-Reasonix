import { MockProvider } from "./mock_provider";
import type { CapabilityScope, McpCatalog, McpDraft, McpDraftServer, McpEntry, McpInstallResult, McpLoad, McpRisk, ScopeLayer } from "./port";

// Fixtures for the add-a-server flow: what a draft looks like once accepted,
// and what the kernel would flag about it before it is.

export function toEntry(s: McpDraftServer): McpEntry {
  return {
    name: s.name,
    state: "idle",
    enabled: true,
    transport: s.transport,
    source: "user config",
    tools: 0,
  };
}

const SECRETY = /auth|token|secret|credential|api[-_]?key|cookie/i;

export function risksOf(servers: McpDraftServer[]): McpRisk[] {
  const out: McpRisk[] = [];
  for (const s of servers) {
    if (s.command) {
      out.push({ server: s.name, kind: "shell", field: "command", detail: [s.command, ...(s.args ?? [])].join(" ") });
    }
    if (s.url) out.push({ server: s.name, kind: "unknown-host", field: "url", detail: s.url });
    for (const [k, v] of Object.entries(s.env ?? {})) {
      if (SECRETY.test(k) && !v.startsWith("$")) {
        out.push({ server: s.name, kind: "secret", field: "env." + k,
          detail: `明文写进配置文件；改成 \${${k.toUpperCase()}} 可以只留在环境变量里` });
      }
    }
  }
  return out;
}

// The external-services half of the fixture. Held in memory like the other
// faces: the switches have to behave with no kernel behind them.
export class MockMcp extends MockProvider {
  private servers: McpEntry[] = [
    {
      name: "time",
      state: "ready",
      enabled: true,
      transport: "stdio",
      source: "built-in",
      description: "Current time in any IANA zone, and conversion between two of them.",
      tools: 2,
      toolList: [
        { name: "get_current_time", description: "Current time in a given IANA timezone.", readOnly: true },
        { name: "convert_time", description: "Convert a wall-clock time between two zones.", readOnly: true },
      ],
    },
    // 待命和连不上的那两个也有说明 —— 那是上次连上时它们自己给的答复，界面里
    // 必须看得出这一点。待命是缓存命中的常态：工具在目录里，进程等第一次调用。
    {
      name: "context7", state: "standby", enabled: true, tools: 1, transport: "http", remembered: true,
      description: "Up-to-date library documentation, pulled per package and version.",
      toolList: [{ name: "get_library_docs", description: "Fetch docs for a resolved library ID.", readOnly: true }],
    },
    {
      // 真实的连不上就是这个长度：端点把整句话都塞进 error，而它有多长不由我们
      // 定 —— 右栏那一行必须扛得住，否则名字先被挤没。
      name: "figma", state: "failed", enabled: true, transport: "http", tools: 1,
      error: "401 unauthorized: figma-mcp needs a personal access token with file_read scope; set FIGMA_TOKEN and reconnect",
      description: "Read Figma files, frames and comments.", remembered: true, stale: true,
      toolList: [{ name: "get_file", description: "Read one Figma file's node tree." }],
    },
  ];

  async mcp(): Promise<McpCatalog> {
    return { servers: this.servers.map((s) => ({ ...s })), scope: await this.capabilityScope() };
  }

  // A repository with several worktrees is the case the scope bar exists for,
  // so the fixture is one rather than a lone folder.
  // Three projects, two of them sharing a name: the case the picker's labels
  // and its override filter exist for.

  async capabilityScope(): Promise<CapabilityScope> {
    return {
      root: "F:\Reasonix",
      name: "Reasonix",
      key: "repo:8f3c1a92d4e05b17",
      repo: true,
      trees: 3,
      branch: "studio",
      overrides: this.servers.filter((s) => s.localOverride).length + this.localSkills.size,
      current: true,
    };
  }

  // A repository with several worktrees is the case the scope bar exists for,
  // so the fixture is one rather than a lone folder.
  // Three projects, two of them sharing a name: the case the picker's labels
  // and its override filter exist for.
  async capabilityScopes(): Promise<CapabilityScope[]> {
    const here = await this.capabilityScope();
    return [
      here,
      { root: "F:\work\api\frontend", name: "frontend", label: "api/frontend",
        key: "repo:1a2b3c4d5e6f7a8b", repo: true, trees: 1, branch: "main", overrides: 1 },
      { root: "F:\side\shop\frontend", name: "frontend", label: "shop/frontend",
        key: "repo:9f8e7d6c5b4a3928", repo: true, trees: 1, branch: "main", overrides: 0 },
      { root: "D:\svn\tuyou_richman", name: "tuyou_richman",
        key: "path:44556677aabbccdd", repo: false, trees: 1, overrides: 2 },
    ];
  }

  async reconnectMcp(name: string) {
    const s = this.servers.find((x) => x.name === name);
    if (!s) return { state: "failed", error: "no configured MCP server named " + name };
    // figma keeps failing: a retry that always works would hide the state the
    // button exists for.
    if (name === "figma") {
      s.state = "failed";
      s.error = "401 unauthorized";
      return { state: "failed", error: s.error };
    }
    s.state = "ready";
    s.error = undefined;
    s.tools = s.tools || 1;
    return { state: "ready", tools: s.tools };
  }

  async setMcpEnabled(name: string, enabled: boolean, scope: ScopeLayer = "project") {
    const s = this.servers.find((x) => x.name === name);
    if (!s) return;
    s.enabled = enabled;
    s.state = enabled ? (s.error ? "failed" : s.tools ? "standby" : "idle") : "disabled";
    s.localOverride = scope === "project";
  }

  async setMcpLoad(name: string, load: McpLoad) {
    const s = this.servers.find((x) => x.name === name);
    if (s) s.alwaysLoad = load === "always";
  }

  async clearMcpOverride(name: string) {
    const s = this.servers.find((x) => x.name === name);
    if (!s) return;
    s.localOverride = false;
    s.enabled = true;
    s.state = s.error ? "failed" : s.tools ? "standby" : "idle";
  }

  // A rough stand-in for internal/ext/mcpsetup: enough shape for the confirmation
  // card to be developed against, not a second implementation of the grammar.

  // A rough stand-in for internal/ext/mcpsetup: enough shape for the confirmation
  // card to be developed against, not a second implementation of the grammar.
  async parseMcp(input: string): Promise<McpDraft> {
    const text = input.trim();
    if (!text) throw new Error("粘贴一段 JSON、一行命令，或者一个 https 地址");
    if (text.startsWith("{")) {
      const doc = JSON.parse(text) as { mcpServers?: Record<string, Record<string, unknown>> };
      const servers = doc.mcpServers ?? (doc as Record<string, Record<string, unknown>>);
      const out: McpDraftServer[] = Object.entries(servers).map(([name, spec]) => ({
        name,
        transport: typeof spec.url === "string" ? "http" : "stdio",
        command: spec.command as string | undefined,
        args: spec.args as string[] | undefined,
        url: spec.url as string | undefined,
        env: spec.env as Record<string, string> | undefined,
      }));
      if (out.length === 0) throw new Error("这段 JSON 里没有 MCP 服务");
      return { servers: out, risks: risksOf(out) };
    }
    if (/^https?:\/\//.test(text)) {
      const host = new URL(text).hostname.replace(/^www\./, "").split(".")[0];
      const server: McpDraftServer = { name: host, transport: "http", url: text };
      return { servers: [server], risks: risksOf([server]) };
    }
    const argv = text.replace(/^[$>%]\s+/, "").split(/\s+/);
    const pkg = argv.slice(1).find((a) => !a.startsWith("-")) ?? argv[0];
    const server: McpDraftServer = {
      name: pkg.split("/").pop()!.split("@")[0] || "mcp-server",
      transport: "stdio",
      command: argv[0],
      args: argv.slice(1),
    };
    return { servers: [server], risks: risksOf([server]) };
  }

  async installMcp(server: McpDraftServer, _scope: "user" | "project"): Promise<McpInstallResult> {
    if (this.servers.some((s) => s.name === server.name)) {
      return { name: server.name, state: "issue", toolCount: 0, action: "retry", message: `已经有一个叫 ${server.name} 的服务了` };
    }
    // A name with "auth" in it stands in for the OAuth path, so the
    // action_required branch is reachable in dev.
    if (/auth|figma/i.test(server.name)) {
      this.servers.push({ ...toEntry(server), state: "failed", error: "401 unauthorized" });
      return { name: server.name, state: "action_required", toolCount: 0, action: "authenticate", message: "需要先授权" };
    }
    this.servers.push({ ...toEntry(server), state: "ready", tools: 3, toolList: [{ name: "one", description: "第一个工具" }, { name: "two", description: "第二个工具", readOnly: true }, { name: "three", description: "第三个工具", destructive: true }] });
    return { name: server.name, state: "ready", toolCount: 3, action: "none", message: "" };
  }

  async removeMcp(name: string) {
    const before = this.servers.length;
    this.servers = this.servers.filter((s) => s.name !== name);
    return { disconnected: before !== this.servers.length, stillConfigured: false };
  }
}
