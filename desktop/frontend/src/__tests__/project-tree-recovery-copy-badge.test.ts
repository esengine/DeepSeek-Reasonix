import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { projectTreeTopicRecoveryCopyCount } from "../lib/projectTreeTopic";
import type { ProjectNode } from "../lib/types";
import { en } from "../locales/en";
import { zh } from "../locales/zh";
import { zhTW } from "../locales/zh-TW";

const topic = (recoveryCopyCount?: number): ProjectNode => ({
  key: "topic_t1",
  kind: "topic",
  label: "Topic",
  topicId: "t1",
  recoveryCopyCount,
});

assert.equal(projectTreeTopicRecoveryCopyCount(topic(2)), 2, "topic rows surface the folded copy count");
assert.equal(projectTreeTopicRecoveryCopyCount(topic(0)), 0, "zero renders no badge");
assert.equal(projectTreeTopicRecoveryCopyCount(topic(undefined)), 0, "missing count renders no badge");
assert.equal(projectTreeTopicRecoveryCopyCount(topic(-1)), 0, "negative count renders no badge");
assert.equal(
  projectTreeTopicRecoveryCopyCount({ key: "session_s1", kind: "session", label: "S", recoveryCopyCount: 3 }),
  0,
  "runtime session rows never carry the badge",
);

const styles = readFileSync(fileURLToPath(new URL("../styles.css", import.meta.url)), "utf8");
assert.match(
  styles,
  /\.project-tree__topic-recovery-copies\s*\{/s,
  "the folded recovery-copy badge has a muted pill style",
);

for (const [name, dict] of Object.entries({ en, zh, zhTW })) {
  const label = dict["projectTree.recoveryCopies"];
  assert.ok(label && label.includes("{count}"), `${name} localizes projectTree.recoveryCopies with a count placeholder`);
}

console.log("  PASS  project tree recovery copy badge contract");
