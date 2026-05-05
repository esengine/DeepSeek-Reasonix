import type { McpServerSummary } from "./slash/types.js";

export function sameMcpServerSummary(a: McpServerSummary, b: McpServerSummary): boolean {
  return a === b || (a.label === b.label && a.spec === b.spec);
}

export function replaceMcpServerSummary(
  servers: McpServerSummary[],
  target: McpServerSummary,
  updated: McpServerSummary,
): McpServerSummary[] {
  return servers.map((server) => (sameMcpServerSummary(server, target) ? updated : server));
}
