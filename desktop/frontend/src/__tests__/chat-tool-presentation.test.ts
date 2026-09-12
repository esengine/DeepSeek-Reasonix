import assert from "node:assert/strict";
import { classifyTool, shellDisplayName, type ToolItem } from "../lib/chatToolPresentation";
import { deriveTurnFiles, fileIdentity } from "../lib/turnFiles";
import { fileResourceCapabilities } from "../lib/fileResource";

const tool = (overrides: Partial<ToolItem>): ToolItem => ({
  kind: "tool", id: "call", name: "unknown", args: "{}", readOnly: true, status: "done", ...overrides,
});

assert.equal(classifyTool(tool({ name: "bash", isShell: true, execution: { shell: "pwsh" } })), "shell");
assert.equal(shellDisplayName(tool({ name: "bash", isShell: true, execution: { shell: "pwsh" } })), "PowerShell");
assert.equal(shellDisplayName(tool({ name: "bash", isShell: true, execution: { shell: "zsh" } })), "Zsh");
assert.equal(shellDisplayName(tool({ name: "bash", isShell: true })), "Terminal");
assert.equal(classifyTool(tool({ name: "write_file" })), "file");
assert.equal(classifyTool(tool({ name: "plugin_write_report", capabilityId: "mcp-tool:plugin:write" })), "tool",
  "untrusted names containing write do not acquire the native file renderer");

const calls: ToolItem[] = [
  tool({ id: "write", name: "write_file", args: '{"path":"./docs/guide.md","content":"hi"}', readOnly: false }),
  tool({ id: "edit", name: "edit_file", args: '{"path":"docs/./guide.md"}', readOnly: false }),
  tool({ id: "noop", name: "write_file", args: '{"path":"noop.txt"}', output: "noop.txt already contains the exact content; no changes made", readOnly: false }),
  tool({ id: "failed", name: "edit_file", args: '{"path":"failed.txt"}', error: "not found", status: "error", readOnly: false }),
  tool({ id: "shell", name: "bash", args: '{"command":"touch guessed.txt"}', readOnly: false, isShell: true }),
  tool({ id: "read", name: "read_file", args: '{"path":"read.txt"}', readOnly: true }),
  tool({ id: "move", name: "move_file", args: '{"source_path":"old.txt","destination_path":"new.txt"}', readOnly: false }),
];
assert.deepEqual(deriveTurnFiles(calls), [
  { path: "./docs/guide.md", toolCallId: "edit", operation: "modified" },
  { path: "new.txt", toolCallId: "move", operation: "written" },
]);
assert.equal(fileIdentity("a/./b/../c.txt"), "a/c.txt");
assert.deepEqual(fileResourceCapabilities({ source: "workspace", hostId: "remote-a", tabId: "tab", toolCallId: "write", path: "report.md" }), {
  preview: true, source: true, browser: false, revealTree: true, copyPath: true, openNative: false, revealNative: false, saveCopy: true,
});
assert.equal(fileResourceCapabilities({ source: "presented", hostId: "local", tabId: "tab", toolCallId: "present", path: "app.html" }).browser, true);

console.log("chat tool presentation: trusted renderer matching, shell labels and file facts passed");
