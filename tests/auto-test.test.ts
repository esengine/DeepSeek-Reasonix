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

function writeNodeTestScript(rootDir: string, script: string): void {
  writeFileSync(
    join(rootDir, "package.json"),
    JSON.stringify({ scripts: { test: script } }),
    "utf8",
  );
}

describe("test-detection", () => {
  let dir: string;

  beforeEach(() => {
    dir = mkdtempSync(join(tmpdir(), "reasonix-test-detection-"));
  });

  afterEach(() => {
    rmSync(dir, { recursive: true, force: true });
  });

  it("detects npm test command by default", () => {
    writeFileSync(
      join(dir, "package.json"),
      JSON.stringify({ scripts: { test: "vitest run" } }),
      "utf8",
    );
    writeFileSync(join(dir, "package-lock.json"), "{}", "utf8");
    expect(detectTestCommand(dir)?.display).toBe("npm test");
  });

  it("detects pnpm test command when pnpm-lock.yaml exists", () => {
    writeFileSync(
      join(dir, "package.json"),
      JSON.stringify({ scripts: { test: "vitest run" } }),
      "utf8",
    );
    writeFileSync(join(dir, "pnpm-lock.yaml"), "", "utf8");
    expect(detectTestCommand(dir)?.display).toBe("pnpm test");
  });

  it("detects pnpm when packageManager field starts with pnpm@", () => {
    writeFileSync(
      join(dir, "package.json"),
      JSON.stringify({
        scripts: { test: "vitest run" },
        packageManager: "pnpm@9.0.0",
      }),
      "utf8",
    );
    expect(detectTestCommand(dir)?.display).toBe("pnpm test");
  });

  it("ignores default npm no-test script", () => {
    writeFileSync(
      join(dir, "package.json"),
      JSON.stringify({ scripts: { test: 'echo "Error: no test specified" && exit 1' } }),
      "utf8",
    );
    expect(detectTestCommand(dir)).toBeNull();
  });

  it("detects pytest when pytest.ini exists", () => {
    writeFileSync(join(dir, "pytest.ini"), "[pytest]\n", "utf8");
    const cmd = detectTestCommand(dir);
    expect(cmd?.kind).toBe("python");
    expect(cmd?.display).toBe("pytest");
  });

  it("detects pytest when pyproject.toml contains pytest config", () => {
    writeFileSync(
      join(dir, "pyproject.toml"),
      '[tool.pytest.ini_options]\nminversion = "6.0"\n',
      "utf8",
    );
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

describe("auto-test runner", () => {
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
    expect(result.reason).toContain("no supported test command detected");
  });

  it("returns passed for successful test command", async () => {
    const scriptPath = join(dir, "test-pass.js");
    writeFileSync(scriptPath, 'console.log("1 passed");\n', "utf8");
    writeNodeTestScript(dir, `node ${scriptPath}`);
    const result = await runGoalAutoTest(dir, { timeoutMs: 10_000 });
    expect(result.status).toBe("passed");
    expect(result.command?.display).toBe("npm test");
    expect(result.counts.passed).toBe(1);
  });

  it("returns failed for failing test command", async () => {
    const scriptPath = join(dir, "test-fail.js");
    writeFileSync(scriptPath, 'console.log("1 failed");\nprocess.exit(1);\n', "utf8");
    writeNodeTestScript(dir, `node ${scriptPath}`);
    const result = await runGoalAutoTest(dir, { timeoutMs: 10_000 });
    expect(result.status).toBe("failed");
    expect(result.counts.failed).toBe(1);
  });

  it("respects custom command override", async () => {
    const scriptPath = join(dir, "pass.js");
    writeFileSync(scriptPath, "process.exit(0);\n", "utf8");
    const result = await runGoalAutoTest(dir, {
      command: {
        kind: "node",
        command: "node",
        args: [scriptPath],
        display: "custom test",
        reason: "override",
      },
      timeoutMs: 10_000,
    });
    expect(result.status).toBe("passed");
    expect(result.command?.display).toBe("custom test");
  });
});

describe("parseVerificationCounts", () => {
  it("extracts passed and failed counts", () => {
    expect(parseVerificationCounts("5 passed, 2 failed")).toEqual({ passed: 5, failed: 2 });
  });

  it("extracts only passed", () => {
    expect(parseVerificationCounts("10 passed")).toEqual({ passed: 10 });
  });

  it("extracts only failed", () => {
    expect(parseVerificationCounts("3 failed")).toEqual({ failed: 3 });
  });

  it("returns empty object for no matches", () => {
    expect(parseVerificationCounts("no tests run")).toEqual({});
  });

  it("takes the last match when multiple exist", () => {
    expect(parseVerificationCounts("5 passed\nre-run: 10 passed")).toEqual({ passed: 10 });
  });
});

describe("formatAutoTestResult", () => {
  it("formats skipped result", () => {
    const formatted = formatAutoTestResult({
      status: "skipped",
      durationMs: 0,
      outputTail: "",
      counts: {},
      reason: "no supported test command detected",
    });
    expect(formatted).toContain("Verification skipped");
  });

  it("formats passed result with counts", () => {
    const formatted = formatAutoTestResult({
      status: "passed",
      command: {
        kind: "node",
        command: "npm",
        args: ["test"],
        display: "npm test",
        reason: "test",
      },
      durationMs: 1234,
      outputTail: "",
      counts: { passed: 5 },
    });
    expect(formatted).toContain("✓ npm test (passed, 1234ms)");
    expect(formatted).toContain("✓ 5 passed");
  });

  it("formats failed result with output tail", () => {
    const formatted = formatAutoTestResult({
      status: "failed",
      command: {
        kind: "node",
        command: "npm",
        args: ["test"],
        display: "npm test",
        reason: "test",
      },
      durationMs: 500,
      outputTail: "Error: test failed",
      counts: { failed: 2 },
    });
    expect(formatted).toContain("✗ npm test (failed, 500ms)");
    expect(formatted).toContain("✗ 2 failed");
    expect(formatted).toContain("Error: test failed");
  });
});
