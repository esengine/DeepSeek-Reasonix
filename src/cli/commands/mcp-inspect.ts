import { McpClient } from "../../mcp/client.js";
import { inspectMcpServer } from "../../mcp/inspect.js";
import type { InspectionReport } from "../../mcp/inspect.js";
import { parseMcpSpec } from "../../mcp/spec.js";
import { SseTransport } from "../../mcp/sse.js";
import { type McpTransport, StdioTransport } from "../../mcp/stdio.js";
import { StreamableHttpTransport } from "../../mcp/streamable-http.js";

export interface McpInspectOptions {
  /** The raw --mcp spec string (e.g. `fs=npx -y @modelcontextprotocol/server-filesystem .`). */
  spec: string;
  /** Emit JSON on stdout instead of the human-readable table. */
  json?: boolean;
}

export async function mcpInspectCommand(opts: McpInspectOptions): Promise<void> {
  const spec = parseMcpSpec(opts.spec);
  const transport: McpTransport =
    spec.transport === "sse"
      ? new SseTransport({ url: spec.url })
      : spec.transport === "streamable-http"
        ? new StreamableHttpTransport({ url: spec.url })
        : new StdioTransport({ command: spec.command, args: spec.args });
  const client = new McpClient({ transport });
  try {
    await client.initialize();
    const report = await inspectMcpServer(client);
    if (opts.json) {
      console.log(JSON.stringify(report, null, 2));
    } else {
      console.log(formatReport(spec.name ?? "(anon)", report));
    }
  } finally {
    await client.close();
  }
}

export function formatMcpInspectFailure(err: unknown): string {
  const error = err instanceof Error ? err : new Error(String(err));
  const message = error.message;
  const code = (error as NodeJS.ErrnoException).code;

  if (code === "ENOENT") {
    const command = message.match(/^spawn\s+([^\s]+)\s+ENOENT$/)?.[1] ?? "the command";
    return `${message} — try: install or verify \`${command}\`, then check the MCP spec's command spelling`;
  }

  if (code === "ECONNREFUSED") {
    const target = message.match(/\b(https?:\/\/\S+|\d+\.\d+\.\d+\.\d+:\d+|localhost:\d+)\b/i)?.[1];
    return `${message} — try: confirm ${target ?? "the MCP server"} is running and the host/port match the spec`;
  }

  if (/^MCP request initialize \(id=\d+\) timed out after \d+ms$/.test(message)) {
    return `${message} — try: confirm the target speaks MCP and completes the handshake before the request timeout`;
  }

  if (/^(empty MCP spec|MCP spec ".*" has name but no command)/.test(message)) {
    return `${message} — try: pass \`name=command args\` or an http(s):// URL`;
  }

  return message;
}

function formatReport(nsName: string, r: InspectionReport): string {
  const lines: string[] = [];
  lines.push(`MCP server [${nsName}]`);
  lines.push(
    `  server     ${r.serverInfo.name || "(unknown)"}${r.serverInfo.version ? ` v${r.serverInfo.version}` : ""}`,
  );
  lines.push(`  protocol   ${r.protocolVersion}`);
  const capKeys = Object.keys(r.capabilities);
  lines.push(`  caps       ${capKeys.length > 0 ? capKeys.join(", ") : "(none advertised)"}`);
  if (r.instructions) {
    lines.push(`  notes      ${r.instructions.trim().slice(0, 200)}`);
  }
  lines.push("");
  lines.push(formatSection("Tools", r.tools, toolLine));
  lines.push(formatSection("Resources", r.resources, resourceLine));
  lines.push(formatSection("Prompts", r.prompts, promptLine));
  return lines.join("\n");
}

function formatSection<T>(
  title: string,
  section: { supported: true; items: T[] } | { supported: false; reason: string },
  render: (item: T) => string,
): string {
  if (!section.supported) {
    return `${title}: (not supported — ${section.reason})`;
  }
  if (section.items.length === 0) {
    return `${title}: (none)`;
  }
  const lines = [`${title} (${section.items.length}):`];
  for (const item of section.items) lines.push(`  ${render(item)}`);
  return lines.join("\n");
}

function toolLine(t: { name: string; description?: string }): string {
  const desc = t.description ? ` — ${oneLine(t.description, 80)}` : "";
  return `· ${t.name}${desc}`;
}

function resourceLine(r: { uri: string; name: string; mimeType?: string }): string {
  const mime = r.mimeType ? ` [${r.mimeType}]` : "";
  return `· ${r.name}${mime}  ${r.uri}`;
}

function promptLine(p: {
  name: string;
  description?: string;
  arguments?: Array<{ name: string; required?: boolean }>;
}): string {
  const argPart =
    p.arguments && p.arguments.length > 0
      ? ` (${p.arguments.map((a) => (a.required ? a.name : `${a.name}?`)).join(", ")})`
      : "";
  const desc = p.description ? ` — ${oneLine(p.description, 80)}` : "";
  return `· ${p.name}${argPart}${desc}`;
}

function oneLine(s: string, max: number): string {
  const flat = s.replace(/\s+/g, " ").trim();
  return flat.length <= max ? flat : `${flat.slice(0, max - 1)}…`;
}
