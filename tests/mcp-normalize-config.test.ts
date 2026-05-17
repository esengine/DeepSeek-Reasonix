import { describe, expect, it } from "vitest";
import { type ReasonixConfig, normalizeMcpConfig } from "../src/config.js";
import type { McpServerSpec } from "../src/mcp/spec.js";
import { SseTransport } from "../src/mcp/sse.js";
import { StdioTransport } from "../src/mcp/stdio.js";
import { StreamableHttpTransport } from "../src/mcp/streamable-http.js";
import { buildTransportFromSpec } from "../src/mcp/transport-from-spec.js";

function names(specs: McpServerSpec[]): (string | null)[] {
  return specs.map((s) => s.name);
}

function findByName(specs: McpServerSpec[], name: string): McpServerSpec | undefined {
  return specs.find((s) => s.name === name);
}

describe("normalizeMcpConfig: legacy-only", () => {
  it("parses mcp: string[] into McpServerSpec[]", () => {
    const cfg: ReasonixConfig = {
      mcp: ["fs=npx -y @scope/fs /tmp", "git=uvx mcp-server-git"],
    };
    const result = normalizeMcpConfig(cfg);
    expect(result).toHaveLength(2);
    expect(names(result)).toEqual(["fs", "git"]);
    const fs = findByName(result, "fs")!;
    expect(fs.transport).toBe("stdio");
    if (fs.transport !== "stdio") throw new Error("unreachable");
    expect(fs.command).toBe("npx");
    expect(fs.args).toEqual(["-y", "@scope/fs", "/tmp"]);
    expect(fs.env).toBeUndefined();
    expect(fs.disabled).toBe(false);
  });

  it("parses anonymous legacy specs", () => {
    const cfg: ReasonixConfig = {
      mcp: ["npx -y @scope/fs /tmp"],
    };
    const result = normalizeMcpConfig(cfg);
    expect(result).toHaveLength(1);
    expect(result[0]!.name).toBeNull();
  });

  it("parses SSE legacy specs", () => {
    const cfg: ReasonixConfig = {
      mcp: ["remote=https://example.com/sse"],
    };
    const result = normalizeMcpConfig(cfg);
    expect(result).toHaveLength(1);
    expect(result[0]!.transport).toBe("sse");
  });

  it("parses streamable-http legacy specs", () => {
    const cfg: ReasonixConfig = {
      mcp: ["edge=streamable+https://edge.example.com/mcp"],
    };
    const result = normalizeMcpConfig(cfg);
    expect(result).toHaveLength(1);
    expect(result[0]!.transport).toBe("streamable-http");
  });

  it("skips invalid legacy specs silently", () => {
    const cfg: ReasonixConfig = {
      mcp: ["fs=npx -y @scope/fs", "", "   "],
    };
    const result = normalizeMcpConfig(cfg);
    expect(result).toHaveLength(1);
    expect(result[0]!.name).toBe("fs");
  });
});

describe("normalizeMcpConfig: object-only", () => {
  it("parses mcpServers into McpServerSpec[]", () => {
    const cfg: ReasonixConfig = {
      mcpServers: {
        github: {
          command: "npx",
          args: ["-y", "@modelcontextprotocol/server-github"],
          env: { GITHUB_TOKEN: "ghp_abc" },
        },
        filesystem: {
          command: "npx",
          args: ["-y", "@modelcontextprotocol/server-filesystem", "/data"],
        },
      },
    };
    const result = normalizeMcpConfig(cfg);
    expect(result).toHaveLength(2);
    expect(names(result)).toEqual(["github", "filesystem"]);
    const gh = findByName(result, "github")!;
    expect(gh.transport).toBe("stdio");
    if (gh.transport !== "stdio") throw new Error("unreachable");
    expect(gh.command).toBe("npx");
    expect(gh.args).toEqual(["-y", "@modelcontextprotocol/server-github"]);
    expect(gh.env).toEqual({ GITHUB_TOKEN: "ghp_abc" });
  });

  it("parses SSE mcpServers with headers", () => {
    const cfg: ReasonixConfig = {
      mcpServers: {
        remote: {
          transport: "sse",
          url: "https://example.com/sse",
          headers: { Authorization: "Bearer tok_abc" },
        },
      },
    };
    const result = normalizeMcpConfig(cfg);
    expect(result).toHaveLength(1);
    const spec = result[0]!;
    expect(spec.transport).toBe("sse");
    if (spec.transport !== "sse") throw new Error("unreachable");
    expect(spec.url).toBe("https://example.com/sse");
    expect(spec.headers).toEqual({ Authorization: "Bearer tok_abc" });
  });

  it("parses streamable-http mcpServers with headers", () => {
    const cfg: ReasonixConfig = {
      mcpServers: {
        edge: {
          transport: "streamable-http",
          url: "https://edge.example.com/mcp",
          headers: { "X-API-Key": "key_xyz" },
        },
      },
    };
    const result = normalizeMcpConfig(cfg);
    expect(result).toHaveLength(1);
    const spec = result[0]!;
    expect(spec.transport).toBe("streamable-http");
    if (spec.transport !== "streamable-http") throw new Error("unreachable");
    expect(spec.url).toBe("https://edge.example.com/mcp");
    expect(spec.headers).toEqual({ "X-API-Key": "key_xyz" });
  });

  it("infers transport from url when transport is omitted", () => {
    const cfg: ReasonixConfig = {
      mcpServers: {
        sseSvc: { url: "https://example.com/sse" },
        stdioSvc: { command: "npx", args: ["-y", "pkg"] },
      },
    };
    const result = normalizeMcpConfig(cfg);
    expect(result).toHaveLength(2);
    const sse = findByName(result, "sseSvc")!;
    expect(sse.transport).toBe("sse");
    const stdio = findByName(result, "stdioSvc")!;
    expect(stdio.transport).toBe("stdio");
  });

  it("strips streamable+ prefix from url when transport is omitted", () => {
    const cfg: ReasonixConfig = {
      mcpServers: {
        edge: { url: "streamable+https://edge.example.com/mcp" },
      },
    };
    const result = normalizeMcpConfig(cfg);
    expect(result).toHaveLength(1);
    const spec = result[0]!;
    expect(spec.transport).toBe("streamable-http");
    if (spec.transport !== "streamable-http") throw new Error("unreachable");
    expect(spec.url).toBe("https://edge.example.com/mcp");
  });
});

describe("normalizeMcpConfig: merge with name conflict", () => {
  it("mcpServers wins silently when both sources have the same name", () => {
    const cfg: ReasonixConfig = {
      mcp: ["fs=npx -y @scope/fs /tmp"],
      mcpServers: {
        fs: {
          command: "node",
          args: ["server.js"],
          env: { CUSTOM: "value" },
        },
      },
    };
    const result = normalizeMcpConfig(cfg);
    expect(result).toHaveLength(1);
    const spec = result[0]!;
    expect(spec.name).toBe("fs");
    if (spec.transport !== "stdio") throw new Error("unreachable");
    // mcpServers wins: command and args from mcpServers
    expect(spec.command).toBe("node");
    expect(spec.args).toEqual(["server.js"]);
    expect(spec.env).toEqual({ CUSTOM: "value" });
  });

  it("mcpServers entry shadows legacy spec with same name", () => {
    const cfg: ReasonixConfig = {
      mcp: ["fs=npx -y @scope/fs /tmp", "git=uvx mcp-server-git"],
      mcpServers: {
        fs: {
          transport: "sse",
          url: "https://mcp.internal/fs/sse",
          headers: { Authorization: "Bearer tok" },
        },
      },
    };
    const result = normalizeMcpConfig(cfg);
    expect(result).toHaveLength(2);
    const fs = findByName(result, "fs")!;
    // mcpServers wins: fs is now SSE, not stdio
    expect(fs.transport).toBe("sse");
    if (fs.transport !== "sse") throw new Error("unreachable");
    expect(fs.url).toBe("https://mcp.internal/fs/sse");
    expect(fs.headers).toEqual({ Authorization: "Bearer tok" });
    // git is still from legacy
    const git = findByName(result, "git")!;
    expect(git.transport).toBe("stdio");
  });
});

describe("normalizeMcpConfig: disabled handling", () => {
  it("honors disabled: true from mcpServers entry", () => {
    const cfg: ReasonixConfig = {
      mcpServers: {
        github: {
          command: "npx",
          args: ["-y", "@scope/github"],
          disabled: true,
        },
      },
    };
    const result = normalizeMcpConfig(cfg);
    expect(result).toHaveLength(1);
    expect(result[0]!.disabled).toBe(true);
  });

  it("honors disabled from legacy mcpDisabled array", () => {
    const cfg: ReasonixConfig = {
      mcp: ["fs=npx -y @scope/fs /tmp"],
      mcpDisabled: ["fs"],
    };
    const result = normalizeMcpConfig(cfg);
    expect(result).toHaveLength(1);
    expect(result[0]!.disabled).toBe(true);
  });

  it("combines both disabled sources", () => {
    const cfg: ReasonixConfig = {
      mcp: ["fs=npx -y @scope/fs /tmp", "git=uvx mcp-server-git"],
      mcpDisabled: ["fs"],
      mcpServers: {
        db: {
          command: "npx",
          args: ["-y", "@scope/db"],
          disabled: true,
        },
      },
    };
    const result = normalizeMcpConfig(cfg);
    expect(result).toHaveLength(3);
    expect(findByName(result, "fs")!.disabled).toBe(true);
    expect(findByName(result, "git")!.disabled).toBe(false);
    expect(findByName(result, "db")!.disabled).toBe(true);
  });

  it("mcpServers disabled:true overrides legacy mcpDisabled for same name", () => {
    const cfg: ReasonixConfig = {
      mcp: ["fs=npx -y @scope/fs"],
      mcpDisabled: [],
      mcpServers: {
        fs: { command: "node", args: ["srv.js"], disabled: true },
      },
    };
    const result = normalizeMcpConfig(cfg);
    const fs = findByName(result, "fs")!;
    expect(fs.disabled).toBe(true);
  });
});

describe("normalizeMcpConfig: env and headers", () => {
  it("env from mcpServers is present on stdio specs", () => {
    const cfg: ReasonixConfig = {
      mcpServers: {
        github: {
          command: "npx",
          args: ["-y", "@scope/github"],
          env: { GITHUB_TOKEN: "ghp_abc" },
        },
      },
    };
    const result = normalizeMcpConfig(cfg);
    const spec = result[0]!;
    if (spec.transport !== "stdio") throw new Error("unreachable");
    expect(spec.env).toEqual({ GITHUB_TOKEN: "ghp_abc" });
  });

  it("env from legacy mcpEnv is present on stdio specs", () => {
    const cfg: ReasonixConfig = {
      mcp: ["github=npx -y @scope/github"],
      mcpEnv: { github: { GITHUB_TOKEN: "ghp_abc" } },
    };
    const result = normalizeMcpConfig(cfg);
    const spec = result[0]!;
    if (spec.transport !== "stdio") throw new Error("unreachable");
    expect(spec.env).toEqual({ GITHUB_TOKEN: "ghp_abc" });
  });

  it("headers from mcpServers are present on SSE specs", () => {
    const cfg: ReasonixConfig = {
      mcpServers: {
        remote: {
          transport: "sse",
          url: "https://example.com/sse",
          headers: { Authorization: "Bearer tok" },
        },
      },
    };
    const result = normalizeMcpConfig(cfg);
    const spec = result[0]!;
    if (spec.transport !== "sse") throw new Error("unreachable");
    expect(spec.headers).toEqual({ Authorization: "Bearer tok" });
  });

  it("headers from mcpServers are present on streamable-http specs", () => {
    const cfg: ReasonixConfig = {
      mcpServers: {
        edge: {
          transport: "streamable-http",
          url: "https://edge.example.com/mcp",
          headers: { "X-API-Key": "key_xyz" },
        },
      },
    };
    const result = normalizeMcpConfig(cfg);
    const spec = result[0]!;
    if (spec.transport !== "streamable-http") throw new Error("unreachable");
    expect(spec.headers).toEqual({ "X-API-Key": "key_xyz" });
  });

  it("anonymous legacy spec has no env overlay", () => {
    const cfg: ReasonixConfig = {
      mcp: ["npx -y @scope/fs"],
      mcpEnv: { anon: { TOKEN: "val" } },
    };
    const result = normalizeMcpConfig(cfg);
    expect(result).toHaveLength(1);
    expect(result[0]!.name).toBeNull();
    if (result[0]!.transport !== "stdio") throw new Error("unreachable");
    expect(result[0]!.env).toBeUndefined();
  });
});

describe("normalizeMcpConfig: headers round-trip into transport", () => {
  it("headers on SSE spec are passed to SseTransport", () => {
    const cfg: ReasonixConfig = {
      mcpServers: {
        remote: {
          transport: "sse",
          url: "https://example.com/sse",
          headers: { Authorization: "Bearer tok" },
        },
      },
    };
    const specs = normalizeMcpConfig(cfg);
    const transport = buildTransportFromSpec(specs[0]!);
    expect(transport).toBeInstanceOf(SseTransport);
  });

  it("headers on streamable-http spec are passed to StreamableHttpTransport", () => {
    const cfg: ReasonixConfig = {
      mcpServers: {
        edge: {
          transport: "streamable-http",
          url: "https://edge.example.com/mcp",
          headers: { "X-API-Key": "key_xyz" },
        },
      },
    };
    const specs = normalizeMcpConfig(cfg);
    const transport = buildTransportFromSpec(specs[0]!);
    expect(transport).toBeInstanceOf(StreamableHttpTransport);
  });

  it("stdio server with headers ignores headers", () => {
    const cfg: ReasonixConfig = {
      mcpServers: {
        local: {
          command: "npx",
          args: ["-y", "@scope/local"],
          headers: { Authorization: "Bearer tok" },
        },
      },
    };
    const specs = normalizeMcpConfig(cfg);
    const spec = specs[0]!;
    if (spec.transport !== "stdio") throw new Error("unreachable");
    // headers should not be present on stdio spec
    expect((spec as Record<string, unknown>).headers).toBeUndefined();
    const transport = buildTransportFromSpec(spec);
    expect(transport).toBeInstanceOf(StdioTransport);
    void transport.close();
  });

  it("env on stdio spec is passed to StdioTransport", () => {
    const cfg: ReasonixConfig = {
      mcpServers: {
        local: {
          command: "node",
          args: ["-e", "console.log('hi')"],
          env: { FOO: "bar" },
        },
      },
    };
    const specs = normalizeMcpConfig(cfg);
    const transport = buildTransportFromSpec(specs[0]!);
    expect(transport).toBeInstanceOf(StdioTransport);
    void transport.close();
  });
});

describe("normalizeMcpConfig: extraLegacy parameter", () => {
  it("extraLegacy replaces cfg.mcp when provided", () => {
    const cfg: ReasonixConfig = {
      mcp: ["fs=npx -y @scope/fs"],
      mcpServers: {
        db: { command: "npx", args: ["-y", "@scope/db"] },
      },
    };
    const result = normalizeMcpConfig(cfg, ["cli=npx -y @scope/cli"]);
    expect(result).toHaveLength(2);
    expect(names(result)).toEqual(["cli", "db"]);
  });

  it("extraLegacy is empty array falls back to cfg.mcp", () => {
    const cfg: ReasonixConfig = {
      mcp: ["fs=npx -y @scope/fs"],
    };
    const result = normalizeMcpConfig(cfg, []);
    expect(result).toHaveLength(1);
    expect(result[0]!.name).toBe("fs");
  });
});
