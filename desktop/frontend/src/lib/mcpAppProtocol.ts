import type { AppBridge } from "@modelcontextprotocol/ext-apps/app-bridge";

export interface MCPAppPresentation {
  server: string;
  tool: string;
  generation: number;
  resourceUri?: string;
  csp?: Record<string, string[]>;
  rawResult?: unknown;
  structured?: unknown;
}

export interface MCPAppInstanceView {
  instanceToken: string;
  tabId: string;
  server: string;
  tool: string;
  outerUrl: string;
  resourceQuery: string;
  resourceDigest: string;
}

export function parseMCPAppArguments(raw: string): Record<string, unknown> {
  try {
    const parsed = JSON.parse(raw) as unknown;
    return parsed !== null && typeof parsed === "object" && !Array.isArray(parsed)
      ? parsed as Record<string, unknown>
      : {};
  } catch {
    return {};
  }
}

export function normalizeMCPAppResult(
  presentation: MCPAppPresentation,
  fallbackOutput: string | undefined,
): Parameters<AppBridge["sendToolResult"]>[0] {
  const raw = presentation.rawResult;
  const result = raw !== null && typeof raw === "object" && !Array.isArray(raw)
    ? { ...(raw as Record<string, unknown>) }
    : {};
  if (!Array.isArray(result.content)) {
    result.content = fallbackOutput ? [{ type: "text", text: fallbackOutput }] : [];
  }
  if (result.structuredContent === undefined && presentation.structured !== undefined) {
    result.structuredContent = presentation.structured;
  }
  return result as Parameters<AppBridge["sendToolResult"]>[0];
}

export function parseMCPAppCallResult(
  raw: string,
): Parameters<AppBridge["sendToolResult"]>[0] {
  const parsed = JSON.parse(raw) as unknown;
  if (parsed === null || typeof parsed !== "object" || Array.isArray(parsed)) {
    throw new Error("MCP App tool result is not an object");
  }
  const result = parsed as Record<string, unknown>;
  if (!Array.isArray(result.content)) {
    throw new Error("MCP App tool result has invalid content");
  }
  return result as Parameters<AppBridge["sendToolResult"]>[0];
}

export function validatedMCPAppLinkOrigin(rawURL: string): string | null {
  try {
    const target = new URL(rawURL);
    if ((target.protocol !== "https:" && target.protocol !== "http:") || target.username || target.password) {
      return null;
    }
    return target.origin;
  } catch {
    return null;
  }
}
