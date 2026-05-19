/** Agent-facing tools for scaffolding skills + MCP servers from chat. Persists via the same paths the wizard / `/skill new` use. */

import { defaultConfigPath, loadResolvedSkillPaths, readConfig, writeConfig } from "../config.js";
import { MCP_CATALOG } from "../mcp/catalog.js";
import { preflightStdioSpec } from "../mcp/preflight.js";
import { type McpSpec, parseMcpSpec } from "../mcp/spec.js";
import { SkillStore } from "../skills.js";
import type { ToolRegistry } from "../tools.js";

export interface ScaffoldToolsOptions {
  homeDir?: string;
  projectRoot?: string;
  /** Override config path — tests point this at a tmp file. */
  configPath?: string;
}

const VALID_SKILL_NAME = /^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$/;
const VALID_SERVER_NAME = /^[a-zA-Z_][a-zA-Z0-9_-]{0,63}$/;
const VALID_TOOL_NAME = /^[a-zA-Z_][a-zA-Z0-9_-]*$/;

export function registerScaffoldTools(
  registry: ToolRegistry,
  opts: ScaffoldToolsOptions = {},
): ToolRegistry {
  const configPath = opts.configPath ?? defaultConfigPath();

  registry.register({
    name: "create_skill",
    description:
      'Scaffold a SKILL.md the user can later invoke via `/skill <name>`. Frontmatter (description / allowed_tools / run_as / model) is filled from structured args here. Use `run_as: "subagent"` for read-and-synthesize playbooks; default inline appends body to parent log. Refuses to overwrite existing skills.',
    parameters: {
      type: "object",
      properties: {
        name: {
          type: "string",
          description:
            "Identifier — letters/digits/`_`/`-`/`.`, 1–64 chars. Becomes filename + frontmatter `name`.",
        },
        description: {
          type: "string",
          description: 'One-liner for the skills index. Lead with the verb ("Run X and …").',
        },
        body: {
          type: "string",
          description: "Markdown playbook. Reference tools by name.",
        },
        scope: {
          type: "string",
          enum: ["project", "global"],
          description:
            "`project` (default) = workspace .reasonix/skills/; `global` = ~/.reasonix/skills/.",
        },
        allowed_tools: {
          type: "array",
          items: { type: "string" },
          description:
            "Optional tool allowlist for `run_as: subagent`. Omit for full inherited toolset.",
        },
        run_as: {
          type: "string",
          enum: ["inline", "subagent"],
          description:
            "inline (default) appends body to parent log. subagent spawns isolated child; only final answer returns.",
        },
        model: {
          type: "string",
          enum: ["deepseek-v4-flash", "deepseek-v4-pro"],
          description:
            "Subagent model override. Default flash; use pro only when the playbook needs it.",
        },
      },
      required: ["name", "description", "body"],
    },
    fn: async (args: {
      name?: unknown;
      description?: unknown;
      body?: unknown;
      scope?: unknown;
      allowed_tools?: unknown;
      run_as?: unknown;
      model?: unknown;
    }) => {
      const name = typeof args.name === "string" ? args.name.trim() : "";
      if (!VALID_SKILL_NAME.test(name)) {
        return JSON.stringify({
          error: `invalid skill name: ${JSON.stringify(name)} — use letters, digits, _, -, .`,
        });
      }
      const description =
        typeof args.description === "string" ? args.description.trim().replace(/\n+/g, " ") : "";
      if (!description) {
        return JSON.stringify({
          error: "create_skill requires a non-empty 'description'",
        });
      }
      const body = typeof args.body === "string" ? args.body : "";
      if (!body.trim()) {
        return JSON.stringify({ error: "create_skill requires a non-empty 'body'" });
      }
      const scope: "project" | "global" =
        args.scope === "global" ? "global" : opts.projectRoot ? "project" : "global";
      const runAs: "inline" | "subagent" = args.run_as === "subagent" ? "subagent" : "inline";
      const allowedTools = parseAllowedTools(args.allowed_tools);
      if (allowedTools && "error" in allowedTools) {
        return JSON.stringify({ error: allowedTools.error });
      }
      const model =
        typeof args.model === "string" && args.model.startsWith("deepseek-")
          ? args.model
          : undefined;

      const content = serializeSkill({
        name,
        description,
        runAs,
        allowedTools: allowedTools ?? undefined,
        model,
        body,
      });

      const store = new SkillStore({
        homeDir: opts.homeDir,
        projectRoot: opts.projectRoot,
        customSkillPaths: opts.projectRoot
          ? loadResolvedSkillPaths(opts.projectRoot, configPath)
          : [],
      });
      const result = store.createWithContent(name, scope, content);
      if ("error" in result) {
        return JSON.stringify({ error: result.error });
      }
      return JSON.stringify({
        success: true,
        path: result.path,
        scope,
        name,
        run_as: runAs,
      });
    },
  });

  registry.register({
    name: "add_mcp_server",
    description:
      'Register a new MCP server in the user\'s config (`mcp` array). Takes effect next session. Use stdio for local commands, sse/streamable-http for remote. Pass `from_catalog` (e.g. "filesystem", "github") to auto-fill command+args from the bundled catalog. Refuses name collisions.',
    parameters: {
      type: "object",
      properties: {
        name: {
          type: "string",
          description:
            "Namespace prefix on every tool. Letters/digits/`_`/`-`, must start with letter or `_`.",
        },
        transport: {
          type: "string",
          enum: ["stdio", "sse", "streamable-http"],
          description:
            "stdio = local command via stdin/stdout; sse / streamable-http = remote. Required unless `from_catalog` is set.",
        },
        command: {
          type: "string",
          description: "Argv[0] for stdio — typically `npx` or a binary path.",
        },
        args: {
          type: "array",
          items: { type: "string" },
          description:
            'Remaining argv for stdio — e.g. `["-y", "@modelcontextprotocol/server-filesystem", "/path/to/dir"]`.',
        },
        url: {
          type: "string",
          description: "Endpoint URL for sse / streamable-http — must be http(s)://.",
        },
        from_catalog: {
          type: "string",
          description:
            "Bundled catalog shortcut: filesystem / memory / github / puppeteer / everything. Fills command+args; user supplies user-args via `args`.",
        },
      },
      required: ["name"],
    },
    fn: async (args: {
      name?: unknown;
      transport?: unknown;
      command?: unknown;
      args?: unknown;
      url?: unknown;
      from_catalog?: unknown;
    }) => {
      const name = typeof args.name === "string" ? args.name.trim() : "";
      if (!VALID_SERVER_NAME.test(name)) {
        return JSON.stringify({
          error: `invalid server name: ${JSON.stringify(name)} — must match [a-zA-Z_][a-zA-Z0-9_-]*`,
        });
      }

      const specStr = buildSpecString({
        name,
        transport: typeof args.transport === "string" ? args.transport : undefined,
        command: typeof args.command === "string" ? args.command : undefined,
        argv: Array.isArray(args.args)
          ? (args.args.filter((a) => typeof a === "string") as string[])
          : undefined,
        url: typeof args.url === "string" ? args.url : undefined,
        fromCatalog: typeof args.from_catalog === "string" ? args.from_catalog : undefined,
      });
      if ("error" in specStr) {
        return JSON.stringify({ error: specStr.error });
      }

      let parsed: McpSpec;
      try {
        parsed = parseMcpSpec(specStr.spec);
      } catch (err) {
        return JSON.stringify({ error: (err as Error).message });
      }
      if (parsed.transport === "stdio") {
        try {
          preflightStdioSpec(parsed);
        } catch (err) {
          return JSON.stringify({ error: (err as Error).message });
        }
      }

      const cfg = readConfig(configPath);
      const existing = cfg.mcp ?? [];
      const collision = existing.find((s) => parseSpecName(s) === name);
      if (collision) {
        return JSON.stringify({
          error: `MCP server ${JSON.stringify(name)} already registered: ${collision}`,
        });
      }
      cfg.mcp = [...existing, specStr.spec];
      writeConfig(cfg, configPath);
      return JSON.stringify({
        success: true,
        name,
        transport: parsed.transport,
        spec: specStr.spec,
        config_path: configPath,
        active_on_next_launch: true,
      });
    },
  });

  return registry;
}

interface SerializeSkillArgs {
  name: string;
  description: string;
  runAs: "inline" | "subagent";
  allowedTools?: readonly string[];
  model?: string;
  body: string;
}

export function serializeSkill(args: SerializeSkillArgs): string {
  const lines: string[] = ["---", `name: ${args.name}`, `description: ${args.description}`];
  if (args.runAs === "subagent") {
    lines.push("runAs: subagent");
  }
  if (args.allowedTools && args.allowedTools.length > 0) {
    lines.push(`allowed-tools: ${args.allowedTools.join(", ")}`);
  }
  if (args.model) {
    lines.push(`model: ${args.model}`);
  }
  lines.push("---", "");
  return `${lines.join("\n")}\n${args.body.trim()}\n`;
}

function parseAllowedTools(raw: unknown): readonly string[] | { error: string } | undefined {
  if (raw === undefined || raw === null) return undefined;
  if (!Array.isArray(raw)) {
    return { error: "'allowed_tools' must be an array of tool-name strings" };
  }
  const out: string[] = [];
  for (const v of raw) {
    if (typeof v !== "string") {
      return { error: "'allowed_tools' entries must be strings" };
    }
    const trimmed = v.trim();
    if (!trimmed) continue;
    if (!VALID_TOOL_NAME.test(trimmed)) {
      return { error: `invalid tool name in allowed_tools: ${JSON.stringify(trimmed)}` };
    }
    out.push(trimmed);
  }
  return out.length > 0 ? out : undefined;
}

interface BuildSpecInput {
  name: string;
  transport?: string;
  command?: string;
  argv?: string[];
  url?: string;
  fromCatalog?: string;
}

function buildSpecString(input: BuildSpecInput): { spec: string } | { error: string } {
  if (input.fromCatalog) {
    const entry = MCP_CATALOG.find((e) => e.name === input.fromCatalog);
    if (!entry) {
      const known = MCP_CATALOG.map((e) => e.name).join(", ");
      return {
        error: `unknown catalog entry: ${JSON.stringify(input.fromCatalog)} — known: ${known}`,
      };
    }
    const userArgs = input.argv ?? [];
    if (entry.userArgs && userArgs.length === 0) {
      return {
        error: `catalog entry "${entry.name}" needs ${entry.userArgs} — pass it via the 'args' parameter`,
      };
    }
    const tail = userArgs.map(quoteIfNeeded).join(" ");
    const body = `npx -y ${entry.package}${tail ? ` ${tail}` : ""}`;
    return { spec: `${input.name}=${body}` };
  }

  const transport = input.transport;
  if (!transport) {
    return { error: "add_mcp_server requires 'transport' (or 'from_catalog')" };
  }
  if (transport === "stdio") {
    if (!input.command || !input.command.trim()) {
      return { error: "stdio transport requires 'command'" };
    }
    const tail = (input.argv ?? []).map(quoteIfNeeded).join(" ");
    const body = `${quoteIfNeeded(input.command.trim())}${tail ? ` ${tail}` : ""}`;
    return { spec: `${input.name}=${body}` };
  }
  if (transport === "sse" || transport === "streamable-http") {
    if (!input.url || !/^https?:\/\//i.test(input.url)) {
      return { error: `${transport} transport requires an http(s):// 'url'` };
    }
    const prefix = transport === "streamable-http" ? "streamable+" : "";
    return { spec: `${input.name}=${prefix}${input.url.trim()}` };
  }
  return { error: `unknown transport: ${JSON.stringify(transport)}` };
}

function parseSpecName(spec: string): string | null {
  const m = spec.trim().match(/^([a-zA-Z_][a-zA-Z0-9_-]*)=/);
  return m ? (m[1] ?? null) : null;
}

function quoteIfNeeded(s: string): string {
  return /\s|"/.test(s) ? `"${s.replace(/\\/g, "\\\\").replace(/"/g, '\\"')}"` : s;
}
