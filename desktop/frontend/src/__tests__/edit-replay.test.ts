// Run: tsx src/__tests__/edit-replay.test.ts

import { replaySubmitText, replaySubmitTextPreservingSelectedContext } from "../lib/editReplay";
import { invocationSegmentsFromMessage, replaceInvocationTextRange, serializeInvocationSubmit, type ComposerInvocation } from "../lib/invocationDisplay";
import { formatSelectedTextContext, formatSelectionLabel, stripSelectionLabels } from "../lib/selectedTextContext";

let passed = 0;
let failed = 0;

function eq(a: unknown, b: unknown, label: string) {
  if (a === b) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

console.log("\nedit replay");

eq(
  replaySubmitText("hidden session context\nvisible prompt", "visible prompt", "visible prompt", "visible prompt"),
  "hidden session context\nvisible prompt",
  "unchanged edits preserve the original submitted text",
);

eq(
  replaySubmitText("hidden session context\nvisible prompt @.reasonix/attachments/a.png", "visible prompt @[a.png](.reasonix/attachments/a.png)", "updated prompt @[a.png](.reasonix/attachments/a.png)", "updated prompt @.reasonix/attachments/a.png"),
  "hidden session context\nupdated prompt @.reasonix/attachments/a.png",
  "edited visible text preserves submit-only prefix and raw attachment refs",
);

eq(
  replaySubmitText(undefined, "visible prompt", "updated prompt", "updated prompt"),
  "updated prompt",
  "messages without hidden submit context use the rebuilt submit text",
);

const selectedReferences = [{ id: "chat-1", text: "selected assistant response" }];
const selectedLabel = formatSelectionLabel(selectedReferences[0]);
const selectedContext = formatSelectedTextContext(selectedReferences);
const selectedDisplay = `visible prompt ${selectedLabel}`;
const selectedEditableDisplay = stripSelectionLabels(selectedDisplay, selectedReferences);
const replayedSelectedEdit = replaySubmitTextPreservingSelectedContext(
  `visible prompt\n\n${selectedContext}`,
  selectedEditableDisplay,
  "updated prompt",
  "updated prompt",
);
eq(
  replayedSelectedEdit,
  `updated prompt\n\n${selectedContext}`,
  "editing a selected-context message preserves the quoted context suffix without submitting its display label",
);

eq(
  replaySubmitText("/reasonix-develop review this change", "review this change", "review the updated change", "review the updated change"),
  "/reasonix-develop review the updated change",
  "editing a structured skill message preserves its slash invocation",
);

const mixedInvocations: ComposerInvocation[] = [
  { id: "primary", offset: 0, command: { name: "general-purpose", description: "", kind: "subagent" } },
  { id: "skill", offset: 2, command: { name: "activity-dynamic-debug", description: "", kind: "skill" } },
  { id: "explore", offset: 3, command: { name: "explore", description: "", kind: "subagent" } },
];
const mixedSubmit = serializeInvocationSubmit("你是再做", mixedInvocations);
eq(
  mixedSubmit,
  "/general-purpose 你是 /activity-dynamic-debug 再 /explore 做",
  "multiple abilities serialize in visual order",
);
const mixedSegments = invocationSegmentsFromMessage("你是再做", mixedSubmit);
eq(
  mixedSegments.filter((segment) => segment.type === "invocation").map((segment) => segment.type === "invocation" ? segment.invocation.name : "").join(","),
  "general-purpose,activity-dynamic-debug,explore",
  "multiple abilities restore from display and submit text",
);
eq(
  mixedSegments.filter((segment) => segment.type === "text").map((segment) => segment.type === "text" ? segment.content : "").join(""),
  "你是再做",
  "restored ability segments preserve visible task text",
);

eq(
  replaySubmitText(mixedSubmit, "你是再做", "请开发并检查", "请开发并检查"),
  "/general-purpose 请开发 /activity-dynamic-debug 并检 /explore 查",
  "editing a mixed ability message preserves every invocation",
);

const boundaryInvocations: ComposerInvocation[] = [
  { id: "first", offset: 0, command: { name: "general-purpose", description: "", kind: "subagent" } },
  { id: "second", offset: 0, command: { name: "explore", description: "", kind: "subagent" } },
];
const insertedAfterFirst = replaceInvocationTextRange("", boundaryInvocations, 0, 0, "task", "first");
eq(
  insertedAfterFirst.invocations.map((invocation) => `${invocation.id}:${invocation.offset}`).join(","),
  "first:0,second:4",
  "inserting after an invocation preserves the selected entity boundary",
);
eq(
  serializeInvocationSubmit(insertedAfterFirst.text, insertedAfterFirst.invocations),
  "/general-purpose task /explore",
  "same-offset entities serialize around inserted text in visual order",
);
eq(
  invocationSegmentsFromMessage("develop x", "/my-formatter develop /explore x")
    .filter((segment) => segment.type === "invocation")
    .map((segment) => segment.type === "invocation" ? segment.invocation.name : "")
    .join(","),
  "my-formatter,explore",
  "spaced English messages restore every inline invocation",
);

const sessionPrefix = "以下是用户引用的历史会话上下文：\n\n[会话：Earlier]\n...\n\n---\n\n当前用户问题：\n";
eq(
  replaySubmitText(
    `${sessionPrefix}/reasonix-develop review this change`,
    "review this change",
    "review the updated change",
    "review the updated change",
  ),
  `${sessionPrefix}/reasonix-develop review the updated change`,
  "editing a structured message keeps the hidden referenced-session prefix",
);

// Hydrated structured messages: reload resolves the recorded display (the
// serialized slash form) while submit is the composed model text, so badge
// restoration relies on the leading-known-token fallback.
const segmentNames = (display: string, submit: string, metadata: Record<string, { kind: "skill" | "subagent" }>) =>
  invocationSegmentsFromMessage(display, submit, metadata)
    .map((segment) => segment.type === "invocation" ? `[${segment.invocation.name}]` : segment.content)
    .join("|");
eq(
  segmentNames("/my-formatter fix the tests", "plan-wrapped composed body", { "my-formatter": { kind: "subagent" } }),
  "[my-formatter]|fix the tests",
  "hydrated slash-form display restores its leading known invocation",
);
eq(
  segmentNames("/my-formatter /explore fix", "composed body", { "my-formatter": { kind: "subagent" }, explore: { kind: "subagent" } }),
  "[my-formatter]|[explore]|fix",
  "hydrated display restores consecutive leading invocations",
);
eq(
  segmentNames("/unknown-name fix", "composed body", {}),
  "/unknown-name fix",
  "unknown leading slash names stay plain text after reload",
);
eq(
  segmentNames("see /my-formatter later", "composed body", { "my-formatter": { kind: "subagent" } }),
  "see /my-formatter later",
  "mid-text slash tokens stay plain text after reload",
);

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
