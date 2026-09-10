// Run: tsx src/__tests__/project-tree-color-sort.test.ts
import { arrangeWorkbenchTree } from "../lib/projectTreePresentation";
import type { ProjectNode } from "../lib/types";

function project(label: string, projectColor?: string, children: ProjectNode[] = [], lastActivityAt = 0): ProjectNode {
  return {
    key: `p-${label}`,
    kind: "project",
    label,
    projectColor,
    children,
    lastActivityAt,
  };
}
function topic(label: string, lastActivityAt = 0): ProjectNode {
  return { key: `t-${label}`, kind: "topic", label, topicId: label, lastActivityAt, createdAt: lastActivityAt };
}

let passed = 0;
let failed = 0;
function eq(actual: unknown, expected: unknown, label: string) {
  if (JSON.stringify(actual) === JSON.stringify(expected)) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}\n`);
    failed += 1;
  }
}

// Sort mode "color" groups projects by color and sinks uncolored ones last,
// preserving stable order within the same color.
function main() {
  const projects = [
    project("blue-b", "blue", [topic("t1", 1)]),
    project("plain-a", undefined, [topic("t2", 2)]),
    project("red-z", "red", [topic("t3", 3)]),
    project("plain-b", undefined, [topic("t4", 4)]),
    project("blue-a", "blue", [topic("t5", 5)]),
    project("red-a", "red", [topic("t6", 6)]),
  ];

  const byColor = arrangeWorkbenchTree(projects, "project", "color").map((n) => n.label);
  eq(byColor, ["red-z", "red-a", "blue-b", "blue-a", "plain-a", "plain-b"],
    "color mode groups by color (red before blue per color order) and sinks uncolored last, stable within color");

  // Non-color modes leave project ordering untouched (arrangeWorkbenchTree keeps
  // `arranged` order for organizeMode === "project" and sortMode !== "color").
  const byUpdated = arrangeWorkbenchTree(projects, "project", "updated").map((n) => n.label);
  eq(byUpdated, ["blue-b", "plain-a", "red-z", "plain-b", "blue-a", "red-a"],
    "updated mode preserves the original project order in project mode");

  process.stdout.write(`\n${passed} passed, ${failed} failed\n`);
  if (failed > 0) process.exitCode = 1;
}

void main();
