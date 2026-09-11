// Run: tsx src/__tests__/project-tree-catalog-completeness.test.ts

import { mergeIncompleteProjectTopicPage, mergeProjectTopicPage } from "../lib/projectTreeTopic";
import type { ProjectNode } from "../lib/types";

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

console.log("\nproject tree catalog completeness");

const completeResidentTopics: ProjectNode[] = [
  { key: "topic-a", kind: "topic", label: "A", topicId: "a", lastActivityAt: 600 },
  { key: "topic-b", kind: "topic", label: "B", topicId: "b", lastActivityAt: 500 },
  { key: "topic-c", kind: "topic", label: "C", topicId: "c", lastActivityAt: 400 },
];
const incompleteTopics = mergeIncompleteProjectTopicPage(completeResidentTopics, [
  { key: "topic-b", kind: "topic", label: "B stale", topicId: "b", lastActivityAt: 100 },
  { key: "topic-d", kind: "topic", label: "D", topicId: "d", lastActivityAt: 700 },
]);
eq(
  incompleteTopics.map((node) => `${node.topicId}:${node.label}:${node.lastActivityAt}`),
  ["a:A:600", "b:B:500", "c:C:400", "d:D:700"],
  "an incomplete catalog page preserves complete timestamps and order while appending discoveries",
);
eq(
  incompleteTopics[1] === completeResidentTopics[1],
  true,
  "an incomplete catalog page keeps resident row identity instead of poisoning runtime metadata",
);
eq(
  mergeProjectTopicPage(
    incompleteTopics,
    [
      { key: "topic-a", kind: "topic", label: "A", topicId: "a", lastActivityAt: 600 },
      { key: "topic-c", kind: "topic", label: "C", topicId: "c", lastActivityAt: 400 },
      { key: "topic-b", kind: "topic", label: "B canonical", topicId: "b", lastActivityAt: 100 },
    ],
    false,
  ).map((node) => `${node.topicId}:${node.lastActivityAt}`),
  ["a:600", "c:400", "b:100"],
  "a complete catalog page can still apply a legitimate activity decrease and canonical order",
);

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
