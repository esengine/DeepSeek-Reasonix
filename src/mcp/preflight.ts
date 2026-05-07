import { type Stats, statSync } from "node:fs";
import type { StdioMcpSpec } from "./spec.js";

const FILESYSTEM_PKG = "@modelcontextprotocol/server-filesystem";

export function preflightStdioSpec(spec: StdioMcpSpec): void {
  const pkgIndex = spec.args.indexOf(FILESYSTEM_PKG);
  if (pkgIndex < 0) return;
  const positional = spec.args.slice(pkgIndex + 1).filter((a) => !a.startsWith("-"));
  for (const dir of positional) {
    let stat: Stats;
    try {
      stat = statSync(dir);
    } catch {
      throw new Error(
        `MCP filesystem sandbox '${dir}' does not exist — create it with: mkdir -p '${dir}'`,
      );
    }
    if (!stat.isDirectory()) {
      throw new Error(`MCP filesystem sandbox '${dir}' exists but is not a directory`);
    }
  }
}
