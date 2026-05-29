import { spawn } from "node:child_process";
import { performance } from "node:perf_hooks";
import { detectTestCommand } from "./test-detection.js";
import { type AutoTestResult, DEFAULT_GOAL_TEST_TIMEOUT_MS } from "./types.js";
import type { DetectedTestCommand, VerificationCounts } from "./types.js";

export interface RunGoalAutoTestOptions {
  timeoutMs?: number;
  command?: DetectedTestCommand | null;
  maxOutputChars?: number;
}

export async function runGoalAutoTest(
  rootDir: string,
  opts: RunGoalAutoTestOptions = {},
): Promise<AutoTestResult> {
  const started = performance.now();
  const command = opts.command === undefined ? detectTestCommand(rootDir) : opts.command;
  if (!command) {
    return {
      status: "skipped",
      durationMs: Math.round(performance.now() - started),
      outputTail: "",
      counts: {},
      reason: "no supported test command detected",
    };
  }

  const timeoutMs = opts.timeoutMs ?? DEFAULT_GOAL_TEST_TIMEOUT_MS;
  const maxOutputChars = opts.maxOutputChars ?? 24_000;
  let output = "";
  let timedOut = false;

  return await new Promise<AutoTestResult>((resolve) => {
    const child = spawn(command.command, command.args, {
      cwd: rootDir,
      env: process.env,
      shell: process.platform === "win32",
    });
    const timer = setTimeout(() => {
      timedOut = true;
      child.kill("SIGTERM");
    }, timeoutMs);

    const append = (chunk: Buffer | string) => {
      output += chunk.toString();
      if (output.length > maxOutputChars) output = output.slice(output.length - maxOutputChars);
    };

    child.stdout?.on("data", append);
    child.stderr?.on("data", append);
    child.on("error", (err) => {
      clearTimeout(timer);
      const outputTail = err.message;
      resolve({
        status: "failed",
        command,
        exitCode: null,
        signal: null,
        timedOut,
        durationMs: Math.round(performance.now() - started),
        outputTail,
        counts: parseVerificationCounts(outputTail),
        reason: err.message,
      });
    });
    child.on("close", (exitCode, signal) => {
      clearTimeout(timer);
      const outputTail = tailLines(output, 40);
      resolve({
        status: !timedOut && exitCode === 0 ? "passed" : "failed",
        command,
        exitCode,
        signal,
        timedOut,
        durationMs: Math.round(performance.now() - started),
        outputTail,
        counts: parseVerificationCounts(output),
      });
    });
  });
}

export function parseVerificationCounts(output: string): VerificationCounts {
  const passed = lastNumberBefore(output, /\b(\d+)\s+passed\b/gi);
  const failed = lastNumberBefore(output, /\b(\d+)\s+failed\b/gi);
  return {
    ...(passed !== undefined ? { passed } : {}),
    ...(failed !== undefined ? { failed } : {}),
  };
}

export function formatAutoTestResult(result: AutoTestResult): string {
  if (result.status === "skipped") {
    return ["Tests:", `- Verification skipped: ${result.reason ?? "not detected"}`].join("\n");
  }
  const command = result.command?.display ?? "test command";
  const status = result.status === "passed" ? "passed" : "failed";
  const lines = [
    "Tests:",
    `${result.status === "passed" ? "✓" : "✗"} ${command} (${status}, ${result.durationMs}ms)`,
  ];
  if (result.counts.passed !== undefined) lines.push(`✓ ${result.counts.passed} passed`);
  if (result.counts.failed !== undefined) lines.push(`✗ ${result.counts.failed} failed`);
  if (result.timedOut) lines.push(`timeout: ${command}`);
  if (result.status === "failed" && result.outputTail.trim()) {
    lines.push("", "Output tail:", result.outputTail.trim());
  }
  return lines.join("\n");
}

function lastNumberBefore(output: string, pattern: RegExp): number | undefined {
  let value: number | undefined;
  for (const match of output.matchAll(pattern)) {
    const n = Number.parseInt(match[1] ?? "", 10);
    if (Number.isFinite(n)) value = n;
  }
  return value;
}

function tailLines(output: string, maxLines: number): string {
  const lines = output.replace(/\r\n/g, "\n").split("\n");
  return lines.slice(-maxLines).join("\n").trim();
}
