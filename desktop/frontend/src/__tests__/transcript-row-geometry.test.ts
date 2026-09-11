// Run: tsx src/__tests__/transcript-row-geometry.test.ts

import assert from "node:assert/strict";
import {
  estimateTranscriptRowGeometry,
  resolveReasoningLayoutVariant,
  resolveToolCardDefaultOpen,
  type TranscriptGeometryEnvironment,
} from "../lib/transcriptRowGeometry";
import type { TranscriptRow } from "../lib/transcriptRows";
import type { Item } from "../lib/useController";

const environment: TranscriptGeometryEnvironment = {
  contentWidth: 960,
  typographySignature: "conversation:14/22|code:12/19|metadata:12/19",
};

function reasoningRow(text: string, layoutVariant: "reasoning-summary" | "reasoning-heading-only" | "reasoning-expanded"): TranscriptRow {
  return {
    kind: "reasoning",
    key: `r:${text.length}`,
    item: { kind: "assistant", id: `a:${text.length}`, text: "", reasoning: text, streaming: false },
    segmentKey: "segment",
    layoutVariant,
  };
}

assert.equal(resolveReasoningLayoutVariant("summary", false), "reasoning-summary");
assert.equal(resolveReasoningLayoutVariant("auto", false), "reasoning-summary");
assert.equal(resolveReasoningLayoutVariant("auto", true), "reasoning-expanded");
assert.equal(resolveReasoningLayoutVariant("expanded", false), "reasoning-expanded");
assert.equal(resolveReasoningLayoutVariant("legacy-collapsed", false), "reasoning-heading-only");
assert.equal(resolveReasoningLayoutVariant("hidden", false), null);
assert.equal(resolveReasoningLayoutVariant("pending", false), null);

const shortCollapsed = estimateTranscriptRowGeometry(reasoningRow("十个字的折叠思考内容", "reasoning-summary"), environment);
const hugeCollapsed = estimateTranscriptRowGeometry(reasoningRow("你".repeat(100_000), "reasoning-summary"), environment);
assert.equal(shortCollapsed, hugeCollapsed, "collapsed reasoning geometry is independent of hidden full text");
assert.equal(shortCollapsed, 64, "summary geometry matches the compact browser contract");
assert.equal(
  estimateTranscriptRowGeometry(reasoningRow("你".repeat(100_000), "reasoning-heading-only"), environment),
  36,
  "legacy collapsed reasoning uses the heading-only contract",
);
assert.ok(
  estimateTranscriptRowGeometry(reasoningRow("你".repeat(100_000), "reasoning-expanded"), environment) > shortCollapsed,
  "expanded reasoning is the only variant that estimates the full body",
);

const markdownAnswer = {
  kind: "answer" as const,
  key: "answer:markdown",
  item: {
    kind: "assistant" as const,
    id: "answer:markdown",
    streaming: false,
    reasoning: "",
    text: "### Geometry block\n\n折叠布局检查完成，answer 保持内容感知估高。\n\n- 中文换行\n- English wrapping",
  },
  layoutVariant: "text-flow" as const,
};
const codeAnswer = {
  ...markdownAnswer,
  key: "answer:code",
  item: {
    ...markdownAnswer.item,
    id: "answer:code",
    text: "### Geometry block\n\n折叠布局检查完成，answer 保持内容感知估高。\n\n```ts\nconst stable = true;\n```",
  },
};
assert.equal(estimateTranscriptRowGeometry(markdownAnswer, environment), 157, "structured Markdown follows its final browser geometry");
assert.equal(estimateTranscriptRowGeometry(codeAnswer, environment), 152, "fenced Markdown uses the padded code-block geometry");

const runningTask: Extract<Item, { kind: "tool" }> = {
  kind: "tool",
  id: "task",
  name: "task",
  args: "{}",
  readOnly: false,
  status: "running",
};
assert.equal(resolveToolCardDefaultOpen(runningTask, 2, "summary"), true, "running nested tools default open");
assert.equal(resolveToolCardDefaultOpen({ ...runningTask, status: "done" }, 2, "summary"), false, "settled nested tools default closed");
assert.equal(resolveToolCardDefaultOpen({
  ...runningTask,
  status: "running",
  subagentProgress: {
    phase: "reasoning",
    reasoning: "working",
    text: "",
    notice: "",
    lastActivityAt: 1,
    truncated: false,
    startedAt: 1,
  },
}, 0, "auto"), true, "auto mode follows live subagent reasoning");
assert.equal(resolveToolCardDefaultOpen({
  ...runningTask,
  status: "running",
  subagentProgress: {
    phase: "responding",
    reasoning: "working",
    text: "first response token",
    notice: "",
    lastActivityAt: 2,
    truncated: false,
    startedAt: 1,
  },
}, 0, "auto"), true, "auto mode keeps a subagent card open for its full running lifecycle");

console.log("transcript row geometry tests passed");
