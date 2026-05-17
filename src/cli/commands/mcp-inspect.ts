import { normalizeMcpConfig, readConfig } from "../../config.js";
import { McpClient } from "../../mcp/client.js";
import { inspectMcpServer } from "../../mcp/inspect.js";
import type { InspectionReport } from "../../mcp/inspect.js";
import { preflightStdioSpec } from "../../mcp/preflight.js";
import { overlayMatchedSpec, parseMcpSpec } from "../../mcp/spec.js";
import { buildTransportFromSpec } from "../../mcp/transport-from-spec.js";

export interface McpInspectOptions {
  /** The raw --mcp spec string (e.g. `fs=npx -y @modelcontextprotocol/server-filesystem .`). */
  spec: string;
  /** Emit JSON on stdout instead of the human-readable table. */
  json?: boolean;
}

export async function mcpInspectCommand(opts: McpInspectOptions): Promise<void> {
  const parsed = parseMcpSpec(opts.spec);
  const cfg = readConfig();
  const normalized = normalizeMcpConfig(cfg);
  const matched = parsed.name ? normalized.find((s) => s.name === parsed.name) : undefined;
  const spec = overlayMatchedSpec(parsed, matched);
  if (spec.transport === "stdio") preflightStdioSpec(spec);
  const transport = buildTransportFromSpec(spec);
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

  if (code === "ENOTFOUND" || code === "EAI_AGAIN") {
    return `${message} — try: confirm the hostname is spelled correctly and DNS resolution is working (check your network/VPN)`;
  }

  if (code === "ECONNRESET") {
    return `${message} — try: retry the request; if it keeps happening, check the server's logs for crashes or rate limits`;
  }

  if (code === "ETIMEDOUT") {
    return `${message} — try: confirm the host is reachable and no firewall/proxy is blocking the port`;
  }

  if (
    code === "CERT_HAS_EXPIRED" ||
    code === "DEPTH_ZERO_SELF_SIGNED_CERT" ||
    code === "UNABLE_TO_VERIFY_LEAF_SIGNATURE" ||
    code === "SELF_SIGNED_CERT_IN_CHAIN"
  ) {
    return `${message} — try: renew or trust the server's TLS certificate, or point the spec at an endpoint with a valid cert`;
  }

  // HTTP non-2xx from SSE / Streamable HTTP transports. Match the three
  // exact shapes those transports emit and surface an auth/endpoint hint.
  const httpStatus = matchTransportHttpStatus(message);
  if (httpStatus !== null) {
    return `${message}${hintForHttpStatus(httpStatus)}`;
  }

  if (/^MCP request initialize \(id=\d+\) timed out after \d+ms$/.test(message)) {
    return `${message} — try: confirm the target speaks MCP and completes the handshake before the request timeout`;
  }

  if (/^(empty MCP spec|MCP spec ".*" has name but no command)/.test(message)) {
    return `${message} — try: pass \`name=command args\` or an http(s):// URL`;
  }

  return message;
}

function matchTransportHttpStatus(message: string): number | null {
  // src/mcp/sse.ts: `SSE handshake <url> → <status> <statusText>`
  // src/mcp/sse.ts: `MCP SSE POST <url> failed: <status> <statusText>`
  // src/mcp/streamable-http.ts: `MCP Streamable HTTP POST <url> → <status> <statusText>...`
  const m =
    message.match(/^SSE handshake \S+ → (\d{3})\b/) ??
    message.match(/^MCP SSE POST \S+ failed: (\d{3})\b/) ??
    message.match(/^MCP Streamable HTTP POST \S+ → (\d{3})\b/);
  return m ? Number(m[1]) : null;
}

function hintForHttpStatus(status: number): string {
  if (status === 401) {
    return " — try: check the spec's auth header (e.g. `Authorization: Bearer …`) or confirm the token isn't expired";
  }
  if (status === 403) {
    return " — try: confirm the credentials have permission to reach this MCP endpoint";
  }
  if (status === 404) {
    return " — try: confirm the endpoint path in the spec matches what the server actually exposes";
  }
  if (status >= 500 && status <= 599) {
    return " — try: retry shortly; if the failure persists, check the MCP server's logs";
  }
  return "";
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
