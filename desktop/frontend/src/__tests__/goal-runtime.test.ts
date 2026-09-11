import { formatGoalWorkTime } from "../lib/goalRuntime";

function equal(actual: unknown, expected: unknown, label: string) {
  if (actual !== expected) throw new Error(`${label}: expected ${String(expected)}, got ${String(actual)}`);
}

equal(formatGoalWorkTime(undefined), "0s", "old payload without workDurationMs");
equal(formatGoalWorkTime(0), "0s", "zero duration");
equal(formatGoalWorkTime(42 * 60_000), "42m", "minute duration");
equal(formatGoalWorkTime((2 * 60 + 7) * 60_000), "2h 7m", "hour duration");

console.log("goal runtime formatting: passed");
