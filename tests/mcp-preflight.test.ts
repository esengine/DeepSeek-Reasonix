import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { preflightStdioSpec } from "../src/mcp/preflight.js";
import type { StdioMcpSpec } from "../src/mcp/spec.js";

function stdio(args: string[]): StdioMcpSpec {
  return { transport: "stdio", name: "fs", command: "npx", args };
}

describe("preflightStdioSpec — filesystem MCP", () => {
  let tmp: string;
  beforeEach(() => {
    tmp = mkdtempSync(join(tmpdir(), "reasonix-preflight-"));
  });
  afterEach(() => {
    rmSync(tmp, { recursive: true, force: true });
  });

  it("no-ops on non-filesystem servers (no package match in args)", () => {
    expect(() =>
      preflightStdioSpec(stdio(["-y", "@modelcontextprotocol/server-memory"])),
    ).not.toThrow();
  });

  it("passes when the sandbox dir exists", () => {
    expect(() =>
      preflightStdioSpec(stdio(["-y", "@modelcontextprotocol/server-filesystem", tmp])),
    ).not.toThrow();
  });

  it("throws an actionable error when the dir is missing", () => {
    const missing = join(tmp, "does-not-exist");
    expect(() =>
      preflightStdioSpec(stdio(["-y", "@modelcontextprotocol/server-filesystem", missing])),
    ).toThrow(/does not exist — create it with: mkdir -p/);
  });

  it("throws when the sandbox path is a file, not a directory", () => {
    const file = join(tmp, "not-a-dir");
    writeFileSync(file, "");
    expect(() =>
      preflightStdioSpec(stdio(["-y", "@modelcontextprotocol/server-filesystem", file])),
    ).toThrow(/exists but is not a directory/);
  });

  it("checks every positional dir, fails on the first missing one", () => {
    const missing = join(tmp, "missing");
    expect(() =>
      preflightStdioSpec(stdio(["-y", "@modelcontextprotocol/server-filesystem", tmp, missing])),
    ).toThrow(new RegExp(`'${missing.replace(/\\/g, "\\\\")}' does not exist`));
  });

  it("ignores flag-shaped args after the package id", () => {
    expect(() =>
      preflightStdioSpec(
        stdio(["-y", "@modelcontextprotocol/server-filesystem", "--verbose", tmp]),
      ),
    ).not.toThrow();
  });
});
