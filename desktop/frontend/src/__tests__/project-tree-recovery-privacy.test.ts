import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const source = (relative: string) => readFileSync(fileURLToPath(new URL(relative, import.meta.url)), "utf8");
const treeSource = source("../components/ProjectTree.tsx");
const topicSource = source("../lib/projectTreeTopic.ts");
const styles = source("../styles.css");

assert.ok(!treeSource.includes("projectTreeTopicRecoveryCopyCount"), "project tree ignores deprecated recovery-copy counts");
assert.ok(!treeSource.includes("project-tree__topic-recovery-copies"), "project tree renders no recovery-copy badge");
assert.ok(!topicSource.includes("projectTreeTopicRecoveryCopyCount"), "badge count helper is removed from the ordinary UI contract");
assert.ok(!styles.includes(".project-tree__topic-recovery-copies"), "obsolete recovery-copy badge recipe is removed");

console.log("  PASS  project tree keeps physical recovery copies private");
