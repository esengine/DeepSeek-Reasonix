import assert from "node:assert/strict";
import { parseDelimitedText } from "../components/WorkspaceCsvPreview";
import { historyMessagesToItems } from "../lib/historyItems";

assert.deepEqual(parseDelimitedText('name,note\nGame,"one, two"\nQuote,"said ""hi"""\n', ","), [
  ["name", "note"],
  ["Game", "one, two"],
  ["Quote", 'said "hi"'],
]);
assert.deepEqual(parseDelimitedText("name\tvalue\na\t1\n", "\t"), [
  ["name", "value"],
  ["a", "1"],
]);

const remoteHistory = historyMessagesToItems([
  { role: "assistant", content: "", toolCalls: [{ id: "present-1", name: "present", arguments: '{"files":[{"path":"game.html"}]}' }] },
  { role: "tool", content: "Presented game.html", toolCallId: "present-1", toolName: "present", presentedFiles: [{ path: "game.html", description: "Game" }] },
], "remote-");
const presentItem = remoteHistory.items.find(item => item.kind === "tool" && item.id.includes("present-1"));
assert.deepEqual(presentItem?.kind === "tool" ? presentItem.presentedFiles : undefined, [{ path: "game.html", description: "Game" }]);

console.log("presented files: CSV, TSV, and remote history projection passed");
