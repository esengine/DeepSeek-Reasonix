// Run: tsx src/__tests__/btw-session-key.test.ts

import { btwSessionKeyForTab, shouldMigrateBtwSessionKey } from "../lib/btwSessionKey";

let passed = 0;
let failed = 0;

function ok(value: boolean, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

function neq(actual: unknown, expected: unknown, label: string) {
  ok(actual !== expected, `${label}: got ${JSON.stringify(actual)}`);
}

function eq(actual: unknown, expected: unknown, label: string) {
  ok(actual === expected, `${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
}

console.log("\nbtw session key");

{
  const sessionA = btwSessionKeyForTab({
    id: "tab-a",
    scope: "project",
    workspaceRoot: "/repo",
    topicId: "topic-a",
    sessionPath: "/repo/.reasonix/sessions/topic-a/session-a.jsonl",
  }, "tab-a");
  const sessionB = btwSessionKeyForTab({
    id: "tab-a",
    scope: "project",
    workspaceRoot: "/repo",
    topicId: "topic-a",
    sessionPath: "/repo/.reasonix/sessions/topic-a/session-b.jsonl",
  }, "tab-a");

  neq(sessionA, sessionB, "same tab and topic get isolated keys for different session paths");
}

{
  const explicitPath = btwSessionKeyForTab({
    id: "tab-a",
    scope: "global",
    topicId: "topic-a",
    sessionPath: "/home/user/.reasonix/sessions/global/session-a.jsonl",
  }, "tab-a");
  const fallbackPath = btwSessionKeyForTab({
    id: "tab-a",
    scope: "global",
    topicId: "topic-a",
    sessionPath: "",
  }, "tab-a", "/home/user/.reasonix/sessions/global/session-a.jsonl");

  eq(fallbackPath, explicitPath, "active meta session path is used as a fallback");
}

{
  const topicKey = btwSessionKeyForTab({
    id: "tab-a",
    scope: "project",
    workspaceRoot: "/repo",
    topicId: "topic-a",
    sessionPath: "",
  }, "tab-a");
  const tabKey = btwSessionKeyForTab({
    id: "tab-a",
    scope: "project",
    workspaceRoot: "/repo",
    topicId: "",
    sessionPath: "",
  }, "tab-a");

  neq(topicKey, tabKey, "topic identity is distinct from tab fallback identity");
}

{
  const topicKey = btwSessionKeyForTab({ id: "tab-a", scope: "project", workspaceRoot: "/repo", topicId: "topic-a" }, "tab-a");
  const sessionA = btwSessionKeyForTab({ id: "tab-a", scope: "project", workspaceRoot: "/repo", topicId: "topic-a", sessionPath: "/repo/a.jsonl" }, "tab-a");
  const sessionB = btwSessionKeyForTab({ id: "tab-a", scope: "project", workspaceRoot: "/repo", topicId: "topic-a", sessionPath: "/repo/b.jsonl" }, "tab-a");
  ok(shouldMigrateBtwSessionKey(topicKey, sessionA), "topic fallback state migrates when a concrete session path arrives");
  ok(!shouldMigrateBtwSessionKey(sessionA, sessionB), "one concrete session never migrates into another session");
}

if (failed > 0) {
  process.stderr.write(`btw session key: ${failed} failed, ${passed} passed\n`);
  process.exit(1);
}

process.stdout.write(`btw session key: ${passed} passed\n`);
