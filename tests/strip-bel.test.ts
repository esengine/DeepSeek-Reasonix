import { afterAll, beforeAll, describe, expect, it } from "vitest";

// strip-bel wraps process.stdout.write by closing over the *previous*
// write function. So pin a capture sink BEFORE importing it; the module
// then forwards transformed chunks into our sink.

const ORIGINAL_PLATFORM = process.platform;
const ORIGINAL_STDOUT_WRITE = process.stdout.write.bind(process.stdout);
const ORIGINAL_STDERR_WRITE = process.stderr.write.bind(process.stderr);

const captured: { stdout: unknown[]; stderr: unknown[] } = { stdout: [], stderr: [] };

beforeAll(async () => {
  Object.defineProperty(process, "platform", { value: "win32", configurable: true });
  process.stdout.write = ((chunk: unknown) => {
    captured.stdout.push(chunk);
    return true;
  }) as typeof process.stdout.write;
  process.stderr.write = ((chunk: unknown) => {
    captured.stderr.push(chunk);
    return true;
  }) as typeof process.stderr.write;
  await import("../src/cli/strip-bel.js");
});

afterAll(() => {
  process.stdout.write = ORIGINAL_STDOUT_WRITE;
  process.stderr.write = ORIGINAL_STDERR_WRITE;
  Object.defineProperty(process, "platform", { value: ORIGINAL_PLATFORM, configurable: true });
});

describe("strip-bel (Windows)", () => {
  it("replaces bare BEL bytes with String Terminator (ESC backslash)", () => {
    captured.stdout.length = 0;
    process.stdout.write("hello\x07world");
    expect(captured.stdout).toEqual(["hello\x1b\\world"]);
  });

  it("preserves OSC semantics — BEL terminator becomes ST terminator", () => {
    captured.stdout.length = 0;
    process.stdout.write("\x1b]7;file:///tmp/x\x07");
    expect(captured.stdout).toEqual(["\x1b]7;file:///tmp/x\x1b\\"]);
  });

  it("applies the same substitution to Buffer chunks", () => {
    captured.stdout.length = 0;
    process.stdout.write(Buffer.from([0x68, 0x07, 0x69]));
    expect(captured.stdout.length).toBe(1);
    expect(Array.from(captured.stdout[0] as Buffer)).toEqual([0x68, 0x1b, 0x5c, 0x69]);
  });

  it("passes BEL-free chunks through unchanged (fast path)", () => {
    captured.stdout.length = 0;
    process.stdout.write("hello world\n");
    expect(captured.stdout).toEqual(["hello world\n"]);
  });

  it("also wraps stderr", () => {
    captured.stderr.length = 0;
    process.stderr.write("warn\x07ing");
    expect(captured.stderr).toEqual(["warn\x1b\\ing"]);
  });
});
