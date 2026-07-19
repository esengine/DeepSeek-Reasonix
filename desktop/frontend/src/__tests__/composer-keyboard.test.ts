// Run: tsx src/__tests__/composer-keyboard.test.ts
//
// Tests for composer prompt-history keyboard gating.

import {
  canUsePromptHistory,
  composerEnterAction,
  insertComposerNewline,
  isFnKeyEvent,
  promptHistoryDirectionFromEvent,
  type PromptHistoryDirection,
} from "../lib/composerKeyboard";
import { resetCustomShortcuts, saveCustomShortcut } from "../lib/keyboardShortcuts";
import type { ComposerInvocation } from "../lib/invocationDisplay";

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

function eligible(
  direction: PromptHistoryDirection | null,
  overrides: Partial<Parameters<typeof canUsePromptHistory>[0]> = {},
) {
  return canUsePromptHistory({
    direction,
    menuOpen: false,
    composing: false,
    altKey: false,
    ctrlKey: false,
    metaKey: false,
    shiftKey: false,
    fnKey: false,
    value: "",
    selectionStart: 0,
    selectionEnd: 0,
    historyIndex: -1,
    ...overrides,
  });
}

console.log("\ncomposerKeyboard");

eq(promptHistoryDirectionFromEvent({ key: "ArrowUp" }), "up", "ArrowUp maps to history up");
eq(promptHistoryDirectionFromEvent({ key: "ArrowDown" }), "down", "ArrowDown maps to history down");
eq(promptHistoryDirectionFromEvent({ key: "PageUp", code: "PageUp", keyCode: 33 }), null, "PageUp does not map to history");
eq(promptHistoryDirectionFromEvent({ key: "Home", code: "Home", keyCode: 36 }), null, "Home does not map to history");
eq(promptHistoryDirectionFromEvent({ key: "Unidentified", keyCode: 38 }), "up", "legacy unidentified keyCode 38 maps up");

eq(isFnKeyEvent({ key: "Fn" }), true, "Fn key is detected");
eq(isFnKeyEvent({ key: "ArrowUp", getModifierState: (key) => key === "Fn" }), true, "Fn modifier is detected");

eq(eligible("up", { value: "", selectionStart: 0, selectionEnd: 0 }), true, "empty input ArrowUp can recall history");
eq(eligible("down", { value: "", selectionStart: 0, selectionEnd: 0 }), false, "empty input ArrowDown does not start history");
eq(eligible("up", { value: "draft", selectionStart: 5, selectionEnd: 5 }), false, "ArrowUp at draft end keeps native textarea movement");
eq(eligible("up", { value: "draft", selectionStart: 0, selectionEnd: 0 }), true, "ArrowUp at first position recalls history");
eq(eligible("down", { value: "history", selectionStart: 7, selectionEnd: 7, historyIndex: 0 }), true, "ArrowDown at recalled history end moves newer");
eq(eligible("down", { value: "history", selectionStart: 3, selectionEnd: 3, historyIndex: 0 }), false, "ArrowDown inside text keeps native movement");
eq(eligible("up", { value: "line1\nline2", selectionStart: 7, selectionEnd: 7 }), false, "ArrowUp inside multiline text keeps native movement");
eq(eligible("up", { value: "history", selectionStart: 7, selectionEnd: 7, historyIndex: 0 }), true, "history mode allows repeated ArrowUp");
eq(eligible("up", { value: "", selectionStart: 0, selectionEnd: 0, fnKey: true }), false, "Fn-modified arrows are not history shortcuts");
eq(eligible("up", { value: "", selectionStart: 0, selectionEnd: 0, shiftKey: true }), false, "Shift+Arrow preserves selection behavior");

console.log("\ncomposerEnterAction");

resetCustomShortcuts();
eq(composerEnterAction({ key: "Enter" }, "darwin"), "send", "plain Enter sends by default");
eq(composerEnterAction({ key: "Enter", shiftKey: true }, "darwin"), "newline-native", "Shift+Enter keeps the native line break by default");
eq(composerEnterAction({ key: "Enter", ctrlKey: true }, "windows"), "send", "Ctrl+Enter keeps the legacy default send behavior");
eq(composerEnterAction({ key: "Enter", metaKey: true }, "darwin"), "send", "Cmd+Enter keeps the legacy default send behavior");
eq(composerEnterAction({ key: "Enter", altKey: true }, "linux"), "send", "Alt+Enter keeps the legacy default send behavior");
eq(composerEnterAction({ key: "a" }, "darwin"), null, "non-Enter keys are ignored");

saveCustomShortcut("composer.newline", { key: "Enter", ctrl: true });
eq(composerEnterAction({ key: "Enter", ctrlKey: true }, "windows"), "newline-insert", "custom Ctrl+Enter inserts a newline manually");
eq(composerEnterAction({ key: "Enter", shiftKey: true }, "windows"), "none", "Shift+Enter does nothing once the newline chord moved to Ctrl+Enter");
eq(composerEnterAction({ key: "Enter" }, "windows"), "send", "plain Enter still sends with a custom newline chord");
resetCustomShortcuts();

saveCustomShortcut("composer.send", { key: "Enter", ctrl: true });
eq(composerEnterAction({ key: "Enter", ctrlKey: true }, "linux"), "send", "custom Ctrl+Enter sends (WeChat-style layout)");
eq(composerEnterAction({ key: "Enter" }, "linux"), "newline-insert", "plain Enter breaks the line when the send chord moved to Ctrl+Enter");
eq(composerEnterAction({ key: "Enter", shiftKey: true }, "linux"), "newline-native", "Shift+Enter still breaks the line in the WeChat-style layout");
eq(composerEnterAction({ key: "Enter", altKey: true }, "linux"), "none", "a custom send chord disables legacy modified-Enter aliases");

resetCustomShortcuts();
eq(composerEnterAction({ key: "Enter" }, "darwin"), "send", "reset restores plain-Enter send");
eq(composerEnterAction({ key: "Enter", shiftKey: true }, "darwin"), "newline-native", "reset restores the Shift+Enter default");

const boundaryInvocations: ComposerInvocation[] = [
  { id: "first", offset: 0, command: { name: "first", description: "First skill", kind: "skill" } },
  { id: "second", offset: 0, command: { name: "second", description: "Second subagent", kind: "subagent" } },
];
const newlineAfterFirst = insertComposerNewline("task", boundaryInvocations, {
  start: 0,
  end: 0,
  afterInvocationId: "first",
});
eq(newlineAfterFirst.text, "\ntask", "custom newline updates the composer text");
eq(
  JSON.stringify(newlineAfterFirst.invocations.map((invocation) => [invocation.id, invocation.offset])),
  JSON.stringify([["first", 0], ["second", 1]]),
  "custom newline stays after the invocation at the caret boundary",
);

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
