import { McpClient } from "../../mcp/client.js";
import { inspectMcpServer } from "../../mcp/inspect.js";
import type { InspectionReport } from "../../mcp/inspect.js";
import { preflightStdioSpec } from "../../mcp/preflight.js";
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
  if (spec.transport === "stdio") preflightStdioSpec(spec);
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

  // --- Spawn / process errors ---

  if (code === "ENOENT") {
    const command = message.match(/^spawn\s+([^\s]+)\s+ENOENT$/)?.[1] ?? "the command";
    return `${message} — try: install or verify \`${command}\`, then check the MCP spec's command spelling`;
  }

  if (code === "EACCES") {
    const command = message.match(/^spawn\s+([^\s]+)\s+EACCES$/)?.[1] ?? "the binary";
    return `${message} — try: \`chmod +x ${command}\` or check the file's execute permission`;
  }

  if (code === "ENOTFOUND" || code === "EAI_AGAIN") {
    const host = message.match(/(https?:\/\/[^\s/]+)/i)?.[1] ?? "the host";
    return `${message} — try: confirm ${host} is reachable, check your network and DNS settings`;
  }

  if (code === "ECONNREFUSED") {
    const target = message.match(/\b(https?:\/\/\S+|\d+\.\d+\.\d+\.\d+:\d+|localhost:\d+)\b/i)?.[1];
    return `${message} — try: confirm ${target ?? "the MCP server"} is running and the host/port match the spec`;
  }

  if (code === "ECONNRESET") {
    return `${message} — try: the MCP server closed the connection unexpectedly. Check if it crashed or timed out`;
  }

  if (code === "ERR_INVALID_URL") {
    return `${message} — try: use a valid URL format, e.g. \`http://localhost:8080\` or \`name=npx -y @modelcontextprotocol/server-name\``;
  }

  // --- MCP request / handshake errors ---

  if (/^MCP request initialize \(id=\d+\) timed out after \d+ms$/.test(message)) {
    return `${message} — try: confirm the target speaks MCP server protocol (startup banner on stderr may show progress)`;
  }

  if (/^MCP SSE POST .+ failed: \d+/.test(message)) {
    const url = message.match(/POST\s+(\S+)/)?.[1] ?? "";
    const status = message.match(/failed:\s+(\d+)/)?.[1] ?? "";
    const statusNum = Number.parseInt(status, 10);
    if (statusNum === 404) {
      return `${message} — try: the endpoint does not support POST. Check the MCP server's session management URL`;
    }
    if (statusNum === 405) {
      return `${message} — try: the endpoint does not accept POST requests. Check if the URL points to an MCP SSE endpoint`;
    }
    return `${message} — try: check if the MCP server at ${url} is healthy and reachable`;
  }

  if (/^SSE connect to .+ failed/.test(message)) {
    const url = message.match(/connect to\s+(\S+)/)?.[1] ?? "";
    return `${message} — try: confirm the SSE endpoint at ${url} is serving an MCP endpoint event`;
  }

  // --- Spec / config errors ---

  if (/^(empty MCP spec|MCP spec ".*" has name but no command)/.test(message)) {
    return `${message} — try: pass \`name=command args\` or an http(s):// URL`;
  }

  if (/^MCP spec ".*" requires a URL for/.test(message)) {
    return `${message} — try: append an http(s):// URL at the end of the spec string`;
  }

  // --- Generic transport errors ---

  if (/^transport error:/.test(message)) {
    return `${message} — try: check that the command is installed and can start without errors`;
  }

  if (/^MCP transport is closed/.test(message)) {
    return `${message} — try: the server closed before inspection completed. Check for startup errors on stderr`;
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
