import { type Stats, statSync } from "node:fs";
import { isAbsolute, resolve } from "node:path";
import type { StdioMcpSpec } from "./spec.js";

const FILESYSTEM_PKG = "@modelcontextprotocol/server-filesystem";

export interface PreflightStdioOptions {
  cwd?: string;
}

function resolveSandboxPath(dir: string, cwd: string | undefined): string {
  if (!cwd || isAbsolute(dir)) return dir;
  return resolve(cwd, dir);
}

function describeResolvedPath(dir: string, resolved: string): string {
  return dir === resolved ? `'${dir}'` : `'${dir}' (resolved to '${resolved}')`;
}

export function preflightStdioSpec(
  spec: StdioMcpSpec & { cwd?: string },
  opts: PreflightStdioOptions = {},
): void {
  const pkgIndex = spec.args.indexOf(FILESYSTEM_PKG);
  if (pkgIndex < 0) return;
  const positional = spec.args.slice(pkgIndex + 1).filter((a) => !a.startsWith("-"));
  const cwd = spec.cwd ?? opts.cwd;
  for (const dir of positional) {
    const resolved = resolveSandboxPath(dir, cwd);
    let stat: Stats;
    try {
      stat = statSync(resolved);
    } catch {
      throw new Error(
        `MCP filesystem sandbox ${describeResolvedPath(dir, resolved)} does not exist — create it with: mkdir -p '${resolved}'`,
      );
    }
    if (!stat.isDirectory()) {
      throw new Error(
        `MCP filesystem sandbox ${describeResolvedPath(dir, resolved)} exists but is not a directory`,
      );
    }
  }
}
