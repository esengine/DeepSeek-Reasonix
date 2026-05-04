import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { ToolRegistry } from "../src/tools.js";
import {
  UnsupportedSyntaxError,
  chainAllowed,
  parseCommandChain,
  runChain,
} from "../src/tools/shell-chain.js";
import { isAllowed, isCommandAllowed, registerShellTools, runCommand } from "../src/tools/shell.js";

describe("parseCommandChain", () => {
  it("returns null on a plain single command", () => {
    expect(parseCommandChain("git status")).toBeNull();
    expect(parseCommandChain("node --version")).toBeNull();
  });

  it("splits on `|`", () => {
    const c = parseCommandChain("git status | grep main");
    expect(c).not.toBeNull();
    expect(c!.segments.map((s) => s.argv)).toEqual([
      ["git", "status"],
      ["grep", "main"],
    ]);
    expect(c!.ops).toEqual(["|"]);
  });

  it("splits on `&&`, `||`, `;`", () => {
    const c = parseCommandChain("a && b || c ; d");
    expect(c!.segments.map((s) => s.argv.join(" "))).toEqual(["a", "b", "c", "d"]);
    expect(c!.ops).toEqual(["&&", "||", ";"]);
  });

  it("preserves quoted operators as literal arguments", () => {
    expect(parseCommandChain('grep "a|b" file')).toBeNull();
    expect(parseCommandChain("grep 'a|b' file")).toBeNull();
  });

  it("passes globs through as literal patterns (no shell expansion)", () => {
    const c = parseCommandChain("ls *.ts | grep test");
    expect(c!.segments[0]!.argv).toEqual(["ls", "*.ts"]);
  });

  it("does NOT split on chain chars embedded inside larger tokens", () => {
    // `--flag=1&2` is one POSIX token; the `&` is a literal byte. Tokens
    // containing `&` / `|` / `;` chars but not at the start are passed
    // through untouched, matching the lenient single-command tokenizer.
    expect(parseCommandChain("cargo run -- --flag=1&2")).toBeNull();
    expect(parseCommandChain("grep a|b file")).toBeNull();
    expect(parseCommandChain("echo a;b")).toBeNull();
  });

  it("allows embedded chain chars inside chain segments", () => {
    const c = parseCommandChain("git status ; cargo run -- --flag=1&2");
    expect(c!.segments.map((s) => s.argv)).toEqual([
      ["git", "status"],
      ["cargo", "run", "--", "--flag=1&2"],
    ]);
    expect(c!.ops).toEqual([";"]);
  });

  it("parses redirects inside a chain segment (now supported)", () => {
    const c1 = parseCommandChain("echo hi ; ls > out.txt");
    expect(c1!.segments[1]!.redirects).toEqual([{ kind: ">", target: "out.txt" }]);
    const c2 = parseCommandChain("git status | wc -l > out.txt");
    expect(c2!.segments[1]!.redirects).toEqual([{ kind: ">", target: "out.txt" }]);
    const c3 = parseCommandChain("a ; cmd 2>&1");
    expect(c3!.segments[1]!.redirects).toEqual([{ kind: "2>&1", target: "" }]);
  });

  it("rejects background `&` discovered inside a chain segment", () => {
    expect(() => parseCommandChain("git status ; long &")).toThrow(/"&" is not supported/);
  });

  it("rejects empty leading segments", () => {
    expect(() => parseCommandChain("; echo hi")).toThrow(/empty segment/);
    expect(() => parseCommandChain("|| cat")).toThrow(/empty segment/);
  });

  it("rejects a chain ending with an operator", () => {
    expect(() => parseCommandChain("echo hi &&")).toThrow(/chain ends with/);
    expect(() => parseCommandChain("echo hi ;")).toThrow(/chain ends with/);
  });

  it("rejects empty middle segments", () => {
    expect(() => parseCommandChain("a ; ; b")).toThrow(/empty segment/);
  });

  it("rejects unclosed quotes inside a chain segment", () => {
    expect(() => parseCommandChain('git status ; echo "open')).toThrow(/unclosed/);
  });
});

describe("chainAllowed", () => {
  it("requires every segment to clear the allowlist", () => {
    const c = parseCommandChain("git status | grep foo")!;
    expect(chainAllowed(c, (seg) => isAllowed(seg))).toBe(true);
  });

  it("rejects when any segment fails the allowlist", () => {
    const c = parseCommandChain("git status | rm -rf dist")!;
    expect(chainAllowed(c, (seg) => isAllowed(seg))).toBe(false);
  });
});

describe("isCommandAllowed", () => {
  it("delegates to isAllowed for plain commands", () => {
    expect(isCommandAllowed("git status")).toBe(true);
    expect(isCommandAllowed("rm -rf /")).toBe(false);
  });

  it("treats every chain segment independently", () => {
    expect(isCommandAllowed("git status | grep main")).toBe(true);
    expect(isCommandAllowed("git status && cargo check")).toBe(true);
    expect(isCommandAllowed("git status | npm install")).toBe(false);
  });

  it("returns false for unsupported syntax (rather than throwing)", () => {
    expect(isCommandAllowed("echo hi > out.txt")).toBe(false);
    expect(isCommandAllowed("echo hi &")).toBe(false);
  });
});

describe("runChain integration", () => {
  let tmp: string;
  beforeEach(() => {
    tmp = mkdtempSync(join(tmpdir(), "reasonix-chain-"));
  });
  afterEach(() => {
    rmSync(tmp, { recursive: true, force: true });
  });

  const opts = (over: Partial<Parameters<typeof runChain>[1]> = {}) => ({
    cwd: tmp,
    timeoutSec: 10,
    maxOutputChars: 32_000,
    ...over,
  });

  it("pipes stdout of one segment into stdin of the next", async () => {
    const chain = parseCommandChain(
      "node -e \"process.stdout.write('alpha\\nbeta\\n')\" | node -e \"let d='';process.stdin.on('data',c=>d+=c);process.stdin.on('end',()=>process.stdout.write(d.toUpperCase()))\"",
    )!;
    const r = await runChain(chain, opts());
    expect(r.exitCode).toBe(0);
    expect(r.output).toContain("ALPHA");
    expect(r.output).toContain("BETA");
  });

  it("`&&` short-circuits when the left side fails", async () => {
    const chain = parseCommandChain(
      'node -e "process.exit(2)" && node -e "process.stdout.write(\'should-not-run\')"',
    )!;
    const r = await runChain(chain, opts());
    expect(r.exitCode).toBe(2);
    expect(r.output).not.toContain("should-not-run");
  });

  it("`&&` runs the right side when the left succeeds", async () => {
    const chain = parseCommandChain(
      'node -e "process.exit(0)" && node -e "process.stdout.write(\'ran\')"',
    )!;
    const r = await runChain(chain, opts());
    expect(r.exitCode).toBe(0);
    expect(r.output).toContain("ran");
  });

  it("`||` runs the right side when the left fails", async () => {
    const chain = parseCommandChain(
      'node -e "process.exit(1)" || node -e "process.stdout.write(\'fallback\')"',
    )!;
    const r = await runChain(chain, opts());
    expect(r.exitCode).toBe(0);
    expect(r.output).toContain("fallback");
  });

  it("`||` skips the right side when the left succeeds", async () => {
    const chain = parseCommandChain(
      "node -e \"process.stdout.write('ok')\" || node -e \"process.stdout.write('SKIPPED')\"",
    )!;
    const r = await runChain(chain, opts());
    expect(r.exitCode).toBe(0);
    expect(r.output).toContain("ok");
    expect(r.output).not.toContain("SKIPPED");
  });

  it("`;` runs both sides regardless of exit code", async () => {
    const chain = parseCommandChain(
      'node -e "process.exit(7); 0" ; node -e "process.stdout.write(\'after\')"',
    )!;
    const r = await runChain(chain, opts());
    expect(r.exitCode).toBe(0);
    expect(r.output).toContain("after");
  });

  it("propagates the LAST executed group's exit code", async () => {
    const chain = parseCommandChain('node -e "process.exit(0)" ; node -e "process.exit(5)"')!;
    const r = await runChain(chain, opts());
    expect(r.exitCode).toBe(5);
  });

  it("merges stderr from earlier pipe segments into the output", async () => {
    const chain = parseCommandChain(
      "node -e \"console.error('warn-from-1'); process.stdout.write('hello')\" | node -e \"let d='';process.stdin.on('data',c=>d+=c);process.stdin.on('end',()=>process.stdout.write(d))\"",
    )!;
    const r = await runChain(chain, opts());
    expect(r.output).toContain("warn-from-1");
    expect(r.output).toContain("hello");
  });

  it("times out a chain whose total runtime exceeds the budget", async () => {
    const chain = parseCommandChain(
      'node -e "setTimeout(()=>{},5000)" ; node -e "setTimeout(()=>{},5000)"',
    )!;
    const r = await runChain(chain, opts({ timeoutSec: 0.2 as unknown as number }));
    expect(r.timedOut).toBe(true);
  });
});

describe("runCommand wired with chains", () => {
  let tmp: string;
  beforeEach(() => {
    tmp = mkdtempSync(join(tmpdir(), "reasonix-chain-rc-"));
  });
  afterEach(() => {
    rmSync(tmp, { recursive: true, force: true });
  });

  it("runs a pipe through the public runCommand API", async () => {
    const r = await runCommand(
      "node -e \"process.stdout.write('hi\\n')\" | node -e \"let d='';process.stdin.on('data',c=>d+=c);process.stdin.on('end',()=>process.stdout.write(d))\"",
      { cwd: tmp, timeoutSec: 10 },
    );
    expect(r.exitCode).toBe(0);
    expect(r.output).toContain("hi");
  });

  it("dispatches a chain through the registered tool when both segments are allowlisted", async () => {
    const registry = new ToolRegistry();
    registerShellTools(registry, { rootDir: tmp, extraAllowed: ["node"] });
    const out = await registry.dispatch(
      "run_command",
      JSON.stringify({
        command:
          "node -e \"process.stdout.write('hi')\" && node -e \"process.stdout.write(' there')\"",
      }),
    );
    expect(out).toMatch(/\[exit 0\]/);
    expect(out).toContain("hi there");
  });

  it("refuses a chain when one segment is not allowlisted", async () => {
    const registry = new ToolRegistry();
    registerShellTools(registry, { rootDir: tmp });
    // Non-allowlisted segment with no confirmation listener throws
    const out = await registry.dispatch(
      "run_command",
      JSON.stringify({ command: "git status | rm -rf dist" }),
    );
    expect(out).toMatch(/no confirmation listener registered/);
  });
});
