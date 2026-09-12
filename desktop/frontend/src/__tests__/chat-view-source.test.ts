import assert from "node:assert/strict";
import { ChatSource, type ChatInput } from "../lib/chatViewSource";
import type { Item } from "../lib/useController";

const items: Item[] = [
  { kind: "user", id: "u1", text: "first", checkpointTurn: 1 },
  { kind: "assistant", id: "a1", text: "settled", reasoning: "reason", streaming: false },
  { kind: "user", id: "u2", text: "second", checkpointTurn: 2 },
  { kind: "assistant", id: "a2", text: "", reasoning: "", streaming: true },
];
const input: ChatInput = { items, running: true, hydrating: false, hasOlder: true, loadingOlder: false };
const source = new ChatSource("test");
source.update(input);
await Promise.resolve();
const order = source.getOrderSnapshot();
const oldAnswer = source.getNodeSnapshot("a1");
let lists = 0, historical = 0, active = 0;
source.subscribeOrder(() => lists++);
source.subscribeNode("a1", () => historical++);
source.subscribeNode("a2", () => active++);
for (let i = 0; i < 120; i++) source.updateLive({ id: "a2", text: `answer ${i}`, reasoning: `thought ${i}`, reasoningComplete: true });
assert.equal(active, 120);
assert.equal(lists, 0);
assert.equal(historical, 0);
assert.equal(source.getOrderSnapshot(), order);
assert.equal(source.getNodeSnapshot("a1"), oldAnswer);

const settled = items.map(item => item.id === "a2" ? { ...item, text: "answer 119", reasoning: "thought 119", streaming: false } as Item : item);
source.update({ ...input, items: settled, running: false });
await Promise.resolve();
assert.equal(source.getOrderSnapshot(), order, "settlement preserves every node host key");
assert.equal(source.getNodeSnapshot("u2:process")?.kind, "process");
const process = source.getNodeSnapshot("u2:process");
assert.ok(process?.kind === "process" && process.collapsed);
source.toggleProcess("u2");
source.update({ ...input, items: [...settled], running: false });
await Promise.resolve();
const expanded = source.getNodeSnapshot("u2:process");
assert.ok(expanded?.kind === "process" && !expanded.collapsed, "manual open survives publication");
const earlier: Item[] = [{ kind: "user", id: "u0", text: "older", checkpointTurn: 0 }, { kind: "assistant", id: "a0", text: "older answer", reasoning: "", streaming: false }];
const beforePrepend = source.getNodeSnapshot("a1");
source.update({ ...input, items: [...earlier, ...settled], running: false });
await Promise.resolve();
assert.equal(source.getNodeSnapshot("a1"), beforePrepend);
assert.deepEqual(source.getOrderSnapshot().slice(-order.length), order);

const warning: Item = { kind: "notice", id: "warning", level: "warn", text: "interrupted" };
source.update({ ...input, items: [...settled, warning], running: false });
await Promise.resolve();
const failed = source.getNodeSnapshot("u2:process");
assert.ok(failed?.kind === "process" && !failed.foldable);
assert.ok(source.getOrderSnapshot().includes("warning"));
source.update({ ...input, items: [settled[1]], running: false });
await Promise.resolve();
const incomplete = source.getNodeSnapshot("history-head:process");
assert.ok(incomplete?.kind === "process" && !incomplete.foldable);
source.update(input);
source.dispose();
const notified = active;
await Promise.resolve();
source.updateLive({ id: "a2", text: "late", reasoning: "", reasoningComplete: true });
assert.equal(active, notified);
assert.deepEqual(source.getOrderSnapshot(), []);
console.log("chat view: stable streaming, settlement, prepend, fold, incomplete history and disposal passed");

const recovered = new ChatSource("recovered-weather");
const weather: Item[] = [
  { kind: "user", id: "weather-user", text: "weather", checkpointTurn: 4 },
  { kind: "tool", id: "failed-fetch", name: "web_fetch", args: "{}", readOnly: true, status: "error", error: "DNS refused" },
  { kind: "assistant", id: "retry-message", text: "Trying another source", reasoning: "Need current data", streaming: false },
  { kind: "notice", id: "permission", level: "info", title: "Permission saved", text: "permission saved to /tmp/reasonix.toml: long command" },
  { kind: "tool", id: "successful-fetch", name: "bash", args: "{}", readOnly: true, status: "done", output: "25 degrees" },
  { kind: "assistant", id: "weather-answer", text: "Sunny, 25 degrees", reasoning: "Sources agree", streaming: false },
];
recovered.update({ ...input, items: weather, running: false });
await Promise.resolve();
const compact = recovered.getNodeSnapshot("weather-user:process");
assert.ok(compact?.kind === "process" && compact.collapsed, "recovered call errors do not prevent completed-turn folding");
assert.equal(compact.toolCallCount, 2);
assert.equal(compact.messageCount, 1);
assert.equal(compact.failureCount, 1, "failed call count remains visible in the collapsed summary");
assert.ok(compact.members.includes("permission"), "system receipts are inside the process range");
assert.ok(!compact.members.includes("weather-answer"), "final answer remains outside the process range");
recovered.toggleProcess("weather-user");
recovered.dispose();
const restored = new ChatSource("recovered-weather");
restored.update({ ...input, items: weather, running: false });
assert.equal((restored.getNodeSnapshot("weather-user:process") as typeof compact).collapsed, false, "manual open survives session revisit");
restored.update({ ...input, items: [...weather, { kind: "notice", id: "stopped", level: "info", text: "Stopped" }], running: false });
assert.equal((restored.getNodeSnapshot("weather-user:process") as typeof compact).foldable, false, "terminal interruption is not hidden by a partial answer");
restored.dispose();

const deliverables = new ChatSource("deliverables");
const deliverableItems: Item[] = [
  { kind: "user", id: "deliverable-user", text: "build a game", checkpointTurn: 5 },
  { kind: "tool", id: "write-game", name: "write_file", args: '{"path":"game.html","content":"game"}', readOnly: false, status: "done", output: "wrote game.html" },
  { kind: "tool", id: "write-notes", name: "write_file", args: '{"path":"notes.md","content":"notes"}', readOnly: false, status: "done", output: "wrote notes.md" },
  { kind: "tool", id: "failed-write", name: "write_file", args: '{"path":"missing.md"}', readOnly: false, status: "error", error: "denied" },
  { kind: "tool", id: "present-old", name: "present", args: '{"files":[{"path":"game.html"}]}', readOnly: true, status: "done", output: "Presented game.html", presentedFiles: [{ path: "game.html", description: "Old description" }] },
  { kind: "tool", id: "present-doc", name: "present", args: '{"files":[{"path":"README.md"}]}', readOnly: true, status: "done", output: "Presented README.md", presentedFiles: [{ path: "README.md", description: "Instructions" }] },
  { kind: "tool", id: "present-new", name: "present", args: '{"files":[{"path":"game.html"}]}', readOnly: true, status: "done", output: "Presented game.html", presentedFiles: [{ path: "game.html", description: "Playable game" }] },
  { kind: "assistant", id: "deliverable-answer", text: "Open `game.html`.", reasoning: "", streaming: false },
];
deliverables.update({ ...input, items: deliverableItems, running: false });
await Promise.resolve();
const tail = deliverables.getNodeSnapshot("deliverable-user:tail");
assert.ok(tail?.kind === "tail");
assert.deepEqual(tail.presentedFiles, [
  { path: "game.html", description: "Playable game", toolCallId: "present-new" },
  { path: "README.md", description: "Instructions", toolCallId: "present-doc" },
]);
assert.deepEqual(tail.modifiedFiles, [
  { path: "notes.md", toolCallId: "write-notes", operation: "written" },
], "presented files suppress duplicate compact mutation entries");
const presentProcess = deliverables.getNodeSnapshot("deliverable-user:process");
assert.ok(presentProcess?.kind === "process" && presentProcess.toolCallCount === 6);
deliverables.dispose();

const filesWithoutAnswer = new ChatSource("files-without-answer");
filesWithoutAnswer.update({ ...input, running: false, items: [
  { kind: "user", id: "files-user", text: "make files", checkpointTurn: 6 },
  { kind: "tool", id: "write-only", name: "write_file", args: '{"path":"draft.txt","content":"draft"}', readOnly: false, status: "done", output: "wrote draft.txt" },
  { kind: "tool", id: "present-only", name: "present", args: '{"files":[{"path":"result.pdf"}]}', readOnly: true, status: "done", output: "Presented result.pdf", presentedFiles: [{ path: "result.pdf" }] },
] });
const noAnswerTail = filesWithoutAnswer.getNodeSnapshot("files-user:tail");
assert.ok(noAnswerTail?.kind === "tail");
assert.equal(noAnswerTail.answerKey, undefined);
assert.deepEqual(noAnswerTail.presentedFiles, [{ path: "result.pdf", toolCallId: "present-only" }]);
assert.deepEqual(noAnswerTail.modifiedFiles, [{ path: "draft.txt", toolCallId: "write-only", operation: "written" }]);
assert.ok(filesWithoutAnswer.getOrderSnapshot().includes("files-user:tail"));
filesWithoutAnswer.dispose();
