import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  formatAutoTestResult,
  parseVerificationCounts,
  runGoalAutoTest,
} from "../src/goal/auto-test.js";
import { detectTestCommand } from "../src/goal/test-detection.js";
import type { AutoTestResult } from "../src/goal/types.js";

describe("test-detection", () => {
  let dir: string;

  beforeEach(() => {
    dir = mkdtempSync(join(tmpdir(), "reasonix-test-detection-"));
  });

  afterEach(() => {
    rmSync(dir, { recursive: true, force: true });
  });

  it("detects node test commands with npm as default", () => {
    writeFileSync(
      join(dir, "package.json"),
      JSON.stringify({ scripts: { test: "vitest run" } }),
      "utf8",
    );
    writeFileSync(join(dir, "package-lock.json"), "{}", "utf8");
    expect(detectTestCommand(dir)?.display).toBe("npm test");
    expect(detectTestCommand(dir)?.kind).toBe("node");
  });

  it("detects pnpm when pnpm-lock.yaml exists", () => {
    writeFileSync(
      join(dir, "package.json"),
      JSON.stringify({ scripts: { test: "vitest run" } }),
      "utf8",
    );
    writeFileSync(join(dir, "pnpm-lock.yaml"), "", "utf8");
    expect(detectTestCommand(dir)?.display).toBe("pnpm test");
  });

  it("detects pnpm when packageManager field is set", () => {
    writeFileSync(
      join(dir, "package.json"),
      JSON.stringify({
        scripts: { test: "vitest run" },
        packageManager: "pnpm@9.0.0",
      }),
      "utf8",
    );
    writeFileSync(join(dir, "package-lock.json"), "{}", "utf8");
    expect(detectTestCommand(dir)?.display).toBe("pnpm test");
  });

  it("ignores default npm no-test script", () => {
    writeFileSync(
      join(dir, "package.json"),
      JSON.stringify({
        scripts: { test: 'echo "Error: no test specified" && exit 1' },
      }),
      "utf8",
    );
    expect(detectTestCommand(dir)).toBeNull();
  });

  it("returns null for package.json without test script", () => {
    writeFileSync(join(dir, "package.json"), JSON.stringify({ scripts: {} }), "utf8");
    expect(detectTestCommand(dir)).toBeNull();
  });

  it("detects pytest when pytest.ini exists", () => {
    writeFileSync(join(dir, "pytest.ini"), "[pytest]", "utf8");
    const cmd = detectTestCommand(dir);
    expect(cmd?.kind).toBe("python");
    expect(cmd?.display).toBe("pytest");
  });

  it("detects pytest when pyproject.toml has pytest config", () => {
    writeFileSync(join(dir, "pyproject.toml"), "[tool.pytest.ini_options]", "utf8");
    const cmd = detectTestCommand(dir);
    expect(cmd?.kind).toBe("python");
    expect(cmd?.display).toBe("pytest");
  });

  it("detects go test when go.mod exists", () => {
    writeFileSync(join(dir, "go.mod"), "module example.com/test\n", "utf8");
    const cmd = detectTestCommand(dir);
    expect(cmd?.kind).toBe("go");
    expect(cmd?.display).toBe("go test ./...");
  });

  it("detects cargo test when Cargo.toml exists", () => {
    writeFileSync(join(dir, "Cargo.toml"), '[package]\nname = "test"\n', "utf8");
    const cmd = detectTestCommand(dir);
    expect(cmd?.kind).toBe("rust");
    expect(cmd?.display).toBe("cargo test");
  });

  it("returns null for empty directory", () => {
    expect(detectTestCommand(dir)).toBeNull();
  });
});

describe("parseVerificationCounts", () => {
  it("parses passed and failed counts", () => {
    expect(parseVerificationCounts("15 passed, 2 failed")).toEqual({
      passed: 15,
      failed: 2,
    });
  });

  it("parses only passed", () => {
    expect(parseVerificationCounts("10 passed")).toEqual({ passed: 10 });
  });

  it("parses only failed", () => {
    expect(parseVerificationCounts("3 failed")).toEqual({ failed: 3 });
  });

  it("returns empty for no counts", () => {
    expect(parseVerificationCounts("some output")).toEqual({});
  });

  it("takes the last occurrence of each count", () => {
    expect(parseVerificationCounts("5 passed\n10 passed\n2 failed")).toEqual({
      passed: 10,
      failed: 2,
    });
  });
});

describe("formatAutoTestResult", () => {
  it("formats skipped result", () => {
    const result: AutoTestResult = {
      status: "skipped",
      durationMs: 0,
      outputTail: "",
      counts: {},
      reason: "no supported test command detected",
    };
    expect(formatAutoTestResult(result)).toContain("Verification skipped");
    expect(formatAutoTestResult(result)).toContain("no supported test command detected");
  });

  it("formats passed result", () => {
    const result: AutoTestResult = {
      status: "passed",
      command: {
        kind: "node",
        command: "npm",
        args: ["test"],
        display: "npm test",
        reason: "package.json test script detected",
      },
      exitCode: 0,
      signal: null,
      timedOut: false,
      durationMs: 1234,
      outputTail: "",
      counts: { passed: 5 },
    };
    const formatted = formatAutoTestResult(result);
    expect(formatted).toContain("✓ npm test (passed, 1234ms)");
    expect(formatted).toContain("✓ 5 passed");
  });

  it("formats failed result with output tail", () => {
    const result: AutoTestResult = {
      status: "failed",
      command: {
        kind: "node",
        command: "npm",
        args: ["test"],
        display: "npm test",
        reason: "package.json test script detected",
      },
      exitCode: 1,
      signal: null,
      timedOut: false,
      durationMs: 500,
      outputTail: "Error: test failed",
      counts: { failed: 2 },
    };
    const formatted = formatAutoTestResult(result);
    expect(formatted).toContain("✗ npm test (failed, 500ms)");
    expect(formatted).toContain("✗ 2 failed");
    expect(formatted).toContain("Error: test failed");
  });

  it("includes timeout info when timed out", () => {
    const result: AutoTestResult = {
      status: "failed",
      command: {
        kind: "node",
        command: "npm",
        args: ["test"],
        display: "npm test",
        reason: "package.json test script detected",
      },
      exitCode: null,
      signal: "SIGTERM",
      timedOut: true,
      durationMs: 120000,
      outputTail: "",
      counts: {},
    };
    expect(formatAutoTestResult(result)).toContain("timeout: npm test");
  });
});

describe("runGoalAutoTest", () => {
  let dir: string;

  beforeEach(() => {
    dir = mkdtempSync(join(tmpdir(), "reasonix-auto-test-"));
  });

  afterEach(() => {
    rmSync(dir, { recursive: true, force: true });
  });

  it("returns skipped when no test command detected", async () => {
    const result = await runGoalAutoTest(dir);
    expect(result.status).toBe("skipped");
    expect(result.reason).toBe("no supported test command detected");
  });

  it("returns skipped when command is explicitly null", async () => {
    const result = await runGoalAutoTest(dir, { command: null });
    expect(result.status).toBe("skipped");
  });

  it("runs passing test and returns passed status", async () => {
    writeFileSync(
      join(dir, "package.json"),
      JSON.stringify({ scripts: { test: "node -e \"console.log('1 passed')\"" } }),
      "utf8",
    );
    writeFileSync(join(dir, "package-lock.json"), "{}", "utf8");
    const result = await runGoalAutoTest(dir);
    expect(result.status).toBe("passed");
    expect(result.exitCode).toBe(0);
    expect(result.counts.passed).toBe(1);
  });

  it("runs failing test and returns failed status", async () => {
    writeFileSync(
      join(dir, "package.json"),
      JSON.stringify({
        scripts: { test: "node -e \"console.log('1 failed'); process.exit(1)\"" },
      }),
      "utf8",
    );
    writeFileSync(join(dir, "package-lock.json"), "{}", "utf8");
    const result = await runGoalAutoTest(dir);
    expect(result.status).toBe("failed");
    expect(result.exitCode).toBe(1);
    expect(result.counts.failed).toBe(1);
  });
});
