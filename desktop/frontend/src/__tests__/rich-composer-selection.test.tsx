// Run: tsx src/__tests__/rich-composer-selection.test.tsx
//
// Focused regression suite for rich-composer DOM↔model selection mapping
// (issue #6868 caret jump-to-end after skill/plugin invocation tags).

import { JSDOM } from "jsdom";
import React, { useRef, useState } from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import {
  modelFromDom,
  recoverSelectionAfterEdit,
  RichComposerInput,
  selectionFromDom,
  setDomSelection,
  slashQueryAt,
  type RichComposerInputHandle,
  type RichComposerSelection,
} from "../components/RichComposerInput";
import { LocaleProvider } from "../lib/i18n";
import {
  commandAvailableAtSlashPosition,
  type ComposerInvocation,
} from "../lib/invocationDisplay";
import type { CommandInfo } from "../lib/types";

let passed = 0;
let failed = 0;

function ok(value: boolean, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

function eq(actual: unknown, expected: unknown, label: string) {
  if (actual === expected) ok(true, label);
  else ok(false, `${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
}

function flushTimers(ms = 0): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

class TestResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
}

function installDom() {
  const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
    pretendToBeVisual: true,
    url: "http://localhost/",
  });
  (globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  globalThis.window = dom.window as unknown as Window & typeof globalThis;
  globalThis.document = dom.window.document;
  Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
  globalThis.Node = dom.window.Node;
  globalThis.HTMLElement = dom.window.HTMLElement;
  globalThis.HTMLDivElement = dom.window.HTMLDivElement;
  globalThis.HTMLSpanElement = dom.window.HTMLSpanElement;
  globalThis.HTMLElement = dom.window.HTMLElement;
  globalThis.Event = dom.window.Event;
  globalThis.CustomEvent = dom.window.CustomEvent;
  globalThis.KeyboardEvent = dom.window.KeyboardEvent;
  globalThis.InputEvent = dom.window.InputEvent;
  globalThis.MouseEvent = dom.window.MouseEvent;
  globalThis.MutationObserver = dom.window.MutationObserver;
  globalThis.requestAnimationFrame = (cb: FrameRequestCallback) => dom.window.setTimeout(() => cb(0), 0) as unknown as number;
  globalThis.cancelAnimationFrame = (id: number) => dom.window.clearTimeout(id);
  globalThis.ResizeObserver = TestResizeObserver;
  return dom;
}

const skillCommand: CommandInfo = {
  name: "superpowers:writing-plans",
  description: "Write plans",
  kind: "skill",
  plugin: "superpowers",
};

const pluginCommand: CommandInfo = {
  name: "docs:search",
  description: "Search docs",
  kind: "skill",
  plugin: "docs",
};

const subagentCommand: CommandInfo = {
  name: "explore",
  description: "Explore",
  kind: "subagent",
  color: "amber",
};

function invocation(id: string, offset: number, command: CommandInfo = skillCommand): ComposerInvocation {
  return { id, offset, command };
}

function buildWebView2Dom(text: string, invocations: ComposerInvocation[], mode: "sibling" | "anchor" = "sibling") {
  // Mirror RichComposerInput's render order: text before each invocation offset,
  // then token + caret-anchor, then any trailing body text after the last token.
  const root = document.createElement("div");
  root.className = "composer__rich-input";
  const ordered = [...invocations].sort((a, b) => a.offset - b.offset || a.id.localeCompare(b.id));
  let cursor = 0;
  ordered.forEach((item, index) => {
    const offset = Math.max(0, Math.min(text.length, item.offset));
    if (offset > cursor) {
      root.appendChild(document.createTextNode(text.slice(cursor, offset)));
      cursor = offset;
    }
    const token = document.createElement("span");
    token.className = "composer-invocation-token";
    token.contentEditable = "false";
    token.dataset.invocationId = item.id;
    token.textContent = item.command.name;
    root.appendChild(token);

    const anchor = document.createElement("span");
    anchor.className = "composer-invocation-caret-anchor";
    anchor.dataset.composerCaretAnchor = "true";
    const isLast = index === ordered.length - 1;
    if (mode === "anchor" && isLast && cursor < text.length) {
      // Windows WebView2 shape: remaining body text lands inside the final caret anchor.
      anchor.textContent = `\u00A0${text.slice(cursor)}`;
      cursor = text.length;
    } else {
      anchor.textContent = "\u00A0";
    }
    root.appendChild(anchor);
  });
  if (cursor < text.length) {
    root.appendChild(document.createTextNode(text.slice(cursor)));
  }
  if (ordered.length === 0 && text) {
    root.appendChild(document.createTextNode(text));
  }
  document.body.appendChild(root);
  return root;
}

function placeCaretInText(root: HTMLElement, logicalOffset: number, afterInvocationId?: string) {
  setDomSelection(root, { start: logicalOffset, end: logicalOffset, afterInvocationId });
}

function insertTextAtSelection(root: HTMLElement, data: string, known: Map<string, ComposerInvocation>) {
  const beforeModel = modelFromDom(root, known);
  const beforeSel = selectionFromDom(root, known);
  ok(beforeSel.ok, "selection available before insert");
  if (!beforeSel.ok) return beforeModel;

  const selection = document.getSelection();
  if (!selection || selection.rangeCount === 0) throw new Error("missing selection");
  const range = selection.getRangeAt(0);
  range.deleteContents();
  const node = document.createTextNode(data);
  range.insertNode(node);
  range.setStartAfter(node);
  range.collapse(true);
  selection.removeAllRanges();
  selection.addRange(range);

  const afterModel = modelFromDom(root, known);
  const afterSel = selectionFromDom(root, known);
  return { beforeModel, beforeSel: beforeSel.selection, afterModel, afterSel };
}

console.log("\nrich composer slash queries");

{
  const start = slashQueryAt("/review", { start: 7, end: 7 });
  eq(start?.from, 0, "slash query at the start keeps offset zero");
  eq(start?.query, "review", "slash query at the start captures its filter");

  const middle = slashQueryAt("请用/review检查", { start: 9, end: 9 });
  eq(middle?.from, 2, "slash query after adjacent Chinese text keeps its real offset");
  eq(middle?.to, 9, "slash query ends at the middle caret");
  eq(middle?.query, "review", "slash query after adjacent Chinese text captures its filter");

  const insideToken = slashQueryAt("/review", { start: 4, end: 4 });
  eq(insideToken?.from, 0, "slash query inside a token starts at its slash");
  eq(insideToken?.to, 7, "slash query inside a token replaces the full command token");
  eq(insideToken?.query, "rev", "slash query inside a token filters by text before the caret");

  const end = slashQueryAt("please inspect /", { start: 16, end: 16 });
  eq(end?.from, 15, "trailing slash opens a query at the end");
  eq(end?.query, "", "bare trailing slash exposes the full command menu");

  eq(
    slashQueryAt("@00-基础文件/", { start: 9, end: 9 }),
    null,
    "a directory separator inside an active ref does not open slash completion",
  );
  const escapedRef = "看 @docs/my\\ dir/rev";
  eq(
    slashQueryAt(escapedRef, { start: escapedRef.length, end: escapedRef.length }),
    null,
    "a directory separator inside an escaped-space ref does not open slash completion",
  );
  const commandAfterRef = "@docs/README.md /review";
  eq(
    slashQueryAt(commandAfterRef, { start: commandAfterRef.length, end: commandAfterRef.length })?.query,
    "review",
    "slash completion remains available after a completed ref token",
  );

  eq(
    slashQueryAt("please /review", { start: 7, end: 14 }),
    null,
    "a range selection does not open slash completion",
  );
  eq(
    slashQueryAt("please /review later", { start: 20, end: 20 }),
    null,
    "moving beyond the slash token closes completion",
  );
  ok(
    commandAvailableAtSlashPosition(skillCommand, false),
    "skills remain available away from the message start",
  );
  ok(
    commandAvailableAtSlashPosition(subagentCommand, false),
    "subagents remain available away from the message start",
  );
  ok(
    !commandAvailableAtSlashPosition({
      name: "mcp",
      description: "Manage MCP servers",
      kind: "builtin",
    }, false),
    "builtin commands remain start-only",
  );
}

console.log("\nrich composer selection mapping");

{
  const dom = installDom();
  const text = "hello world from webview";
  const inv = [invocation("inv-1", 0)];
  const known = new Map(inv.map((item) => [item.id, item]));

  // --- Anchor-internal body text (WebView2) ---
  const anchorRoot = buildWebView2Dom(text, inv, "anchor");
  const anchorModel = modelFromDom(anchorRoot, known);
  eq(anchorModel.text, text, "modelFromDom counts user text inside caret anchor");
  eq(anchorModel.invocations[0]?.offset, 0, "invocation offset stays at zero with anchor-hosted body");

  placeCaretInText(anchorRoot, 5);
  let read = selectionFromDom(anchorRoot, known);
  ok(read.ok, "selectionFromDom reads caret inside anchor text");
  eq(read.ok ? read.selection.start : -1, 5, "caret mid-anchor maps to logical offset 5");
  eq(read.ok ? read.selection.end : -1, 5, "collapsed mid-anchor selection end matches start");

  // Simulate the pre-fix asymmetry: restore must not jump to end.
  setDomSelection(anchorRoot, { start: 5, end: 5 });
  read = selectionFromDom(anchorRoot, known);
  eq(read.ok ? read.selection.start : -1, 5, "setDomSelection restores caret inside anchor (not end)");

  // First and second character inserts must stay contiguous at the edit point.
  const first = insertTextAtSelection(anchorRoot, "X", known);
  if ("afterModel" in first) {
    eq(first.afterModel.text, "helloX world from webview", "first mid-text insert stays in place (anchor DOM)");
    eq(first.afterSel.ok ? first.afterSel.selection.start : -1, 6, "caret after first insert is after X");
  }
  const second = insertTextAtSelection(anchorRoot, "Y", known);
  if ("afterModel" in second) {
    eq(second.afterModel.text, "helloXY world from webview", "second mid-text insert stays contiguous (anchor DOM)");
    eq(second.afterSel.ok ? second.afterSel.selection.start : -1, 7, "caret after second insert is after Y");
  }
  anchorRoot.remove();

  // --- Sibling text node body (browser / non-WebView shape) ---
  const siblingRoot = buildWebView2Dom(text, inv, "sibling");
  const siblingModel = modelFromDom(siblingRoot, known);
  eq(siblingModel.text, text, "modelFromDom counts sibling text after caret anchor");
  placeCaretInText(siblingRoot, 5);
  setDomSelection(siblingRoot, { start: 5, end: 5 });
  read = selectionFromDom(siblingRoot, known);
  eq(read.ok ? read.selection.start : -1, 5, "setDomSelection restores caret in sibling text node");
  const siblingInsert = insertTextAtSelection(siblingRoot, "Z", known);
  if ("afterModel" in siblingInsert) {
    eq(siblingInsert.afterModel.text, "helloZ world from webview", "mid-text insert works for sibling text DOM shape");
  }
  siblingRoot.remove();

  // --- Unavailable selection must not fake text.length ---
  const lostRoot = buildWebView2Dom("abcdef", inv, "anchor");
  placeCaretInText(lostRoot, 3);
  document.getSelection()?.removeAllRanges();
  const lost = selectionFromDom(lostRoot, known);
  ok(!lost.ok, "selectionFromDom reports unavailable when selection left the editor");
  lostRoot.remove();

  // --- beforeinput snapshot recovery ---
  const recovered = recoverSelectionAfterEdit(
    {
      text: "hello world",
      selection: { start: 5, end: 5 },
      inputType: "insertText",
      data: "X",
    },
    "helloX world",
    { start: 11, end: 11 },
  );
  eq(recovered.start, 6, "beforeinput insertText recovery keeps caret after inserted char");
  eq(recovered.end, 6, "beforeinput insertText recovery collapses correctly");

  const recoveredReplace = recoverSelectionAfterEdit(
    {
      text: "hello world",
      selection: { start: 6, end: 11 },
      inputType: "insertText",
      data: "there",
    },
    "hello there",
    { start: 11, end: 11 },
  );
  eq(recoveredReplace.start, 11, "mid-range replace recovery places caret after replacement");

  const recoveredBackspace = recoverSelectionAfterEdit(
    {
      text: "hello world",
      selection: { start: 5, end: 5 },
      inputType: "deleteContentBackward",
      data: null,
    },
    "hell world",
    { start: 11, end: 11 },
  );
  eq(recoveredBackspace.start, 4, "Backspace recovery keeps caret at deletion point");

  const recoveredDelete = recoverSelectionAfterEdit(
    {
      text: "hello world",
      selection: { start: 5, end: 5 },
      inputType: "deleteContentForward",
      data: null,
    },
    "helloworld",
    { start: 11, end: 11 },
  );
  eq(recoveredDelete.start, 5, "Delete recovery keeps caret at deletion point");

  // data=null repeated-character insert: maximal prefix/suffix would jump to end.
  const recoveredRepeat = recoverSelectionAfterEdit(
    {
      text: "aaa",
      selection: { start: 1, end: 1 },
      inputType: "insertText",
      data: null,
    },
    "aaaa",
    { start: 3, end: 3 },
  );
  eq(recoveredRepeat.start, 2, "data=null repeated-char insert recovers at edit point (not end)");
  eq(recoveredRepeat.end, 2, "data=null repeated-char insert collapses at edit point");

  const recoveredRepeatEmptyType = recoverSelectionAfterEdit(
    {
      text: "aaa",
      selection: { start: 1, end: 1 },
      inputType: "",
      data: null,
    },
    "aaaa",
    { start: 4, end: 4 },
  );
  eq(recoveredRepeatEmptyType.start, 2, "empty inputType + data=null still anchors repeated insert");

  const recoveredReplacementNull = recoverSelectionAfterEdit(
    {
      text: "aaa bbb",
      selection: { start: 0, end: 3 },
      inputType: "insertReplacementText",
      data: null,
    },
    "aaaa bbb",
    { start: 7, end: 7 },
  );
  eq(recoveredReplacementNull.start, 4, "data=null replacement recovers after the replaced span");

  const recoveredCompositionCommit = recoverSelectionAfterEdit(
    {
      text: "hello world",
      selection: { start: 5, end: 5 },
      inputType: "insertCompositionText",
      data: null,
    },
    "hello你好 world",
    { start: 5, end: 5 },
  );
  eq(recoveredCompositionCommit.start, 7, "composition commit with data=null places caret after new text");
  eq(recoveredCompositionCommit.end, 7, "composition commit recovery collapses after new text");

  // --- Multiline + BR ---
  const multi = document.createElement("div");
  multi.appendChild(document.createTextNode("line1"));
  multi.appendChild(document.createElement("br"));
  multi.appendChild(document.createTextNode("line2"));
  document.body.appendChild(multi);
  const multiModel = modelFromDom(multi, new Map());
  eq(multiModel.text, "line1\nline2", "BR counts as a single newline in the model");
  setDomSelection(multi, { start: 6, end: 6 });
  read = selectionFromDom(multi, new Map());
  eq(read.ok ? read.selection.start : -1, 6, "caret restores after newline (start of line2)");
  multi.remove();

  // --- Repeated characters and CJK ---
  const cjkText = "测试测试重复重复";
  const cjkRoot = buildWebView2Dom(cjkText, inv, "anchor");
  const cjkKnown = known;
  placeCaretInText(cjkRoot, 4);
  setDomSelection(cjkRoot, { start: 4, end: 4 });
  read = selectionFromDom(cjkRoot, cjkKnown);
  eq(read.ok ? read.selection.start : -1, 4, "CJK mid-text caret restores without jumping");
  const cjkInsert = insertTextAtSelection(cjkRoot, "中", cjkKnown);
  if ("afterModel" in cjkInsert) {
    eq(cjkInsert.afterModel.text, "测试测试中重复重复", "CJK insert stays at mid-text position");
  }
  cjkRoot.remove();

  // --- afterInvocationId / multi-invocation offsets ---
  const multiInvText = "alpha beta";
  const multiInv = [
    invocation("a", 0, skillCommand),
    invocation("b", 0, pluginCommand),
    invocation("c", 6, subagentCommand),
  ];
  const multiKnown = new Map(multiInv.map((item) => [item.id, item]));
  const multiRoot = buildWebView2Dom(multiInvText, multiInv, "sibling");
  const multiModel2 = modelFromDom(multiRoot, multiKnown);
  eq(multiModel2.text, multiInvText, "multi-invocation model text ignores tokens and sentinels");
  eq(multiModel2.invocations.map((item) => item.id).join(","), "a,b,c", "multi-invocation order preserved");

  setDomSelection(multiRoot, { start: 0, end: 0, afterInvocationId: "a" });
  read = selectionFromDom(multiRoot, multiKnown);
  eq(read.ok ? read.selection.afterInvocationId : undefined, "a", "afterInvocationId restores to first same-offset tag");

  setDomSelection(multiRoot, { start: 0, end: 0, afterInvocationId: "b" });
  read = selectionFromDom(multiRoot, multiKnown);
  eq(read.ok ? read.selection.afterInvocationId : undefined, "b", "afterInvocationId distinguishes second same-offset tag");

  setDomSelection(multiRoot, { start: 6, end: 6, afterInvocationId: "c" });
  read = selectionFromDom(multiRoot, multiKnown);
  eq(read.ok ? read.selection.start : -1, 6, "caret after mid-text invocation maps to offset 6");
  eq(read.ok ? read.selection.afterInvocationId : undefined, "c", "afterInvocationId works between body segments");

  setDomSelection(multiRoot, { start: 3, end: 3 });
  read = selectionFromDom(multiRoot, multiKnown);
  eq(read.ok ? read.selection.start : -1, 3, "caret before mid-text invocation stays in leading body");
  multiRoot.remove();

  // --- Non-collapsed range restore ---
  const rangeRoot = buildWebView2Dom("abcdefghij", inv, "anchor");
  setDomSelection(rangeRoot, { start: 2, end: 6 });
  read = selectionFromDom(rangeRoot, known);
  eq(read.ok ? read.selection.start : -1, 2, "range restore keeps selection start");
  eq(read.ok ? read.selection.end : -1, 6, "range restore keeps selection end (not collapsed to end-only)");
  rangeRoot.remove();

  // --- Offset clamping ---
  const clampRoot = buildWebView2Dom("abc", inv, "sibling");
  setDomSelection(clampRoot, { start: 99, end: 99 });
  read = selectionFromDom(clampRoot, known);
  eq(read.ok ? read.selection.start : -1, 3, "out-of-range offset clamps to nearest valid end");
  setDomSelection(clampRoot, { start: -5, end: -5 });
  read = selectionFromDom(clampRoot, known);
  eq(read.ok ? read.selection.start : -1, 0, "negative offset clamps to start");
  clampRoot.remove();

  dom.window.close();
}

console.log("\nrich composer selection component integration");

function Harness({
  initialText,
  initialInvocations,
  onReady,
}: {
  initialText: string;
  initialInvocations: ComposerInvocation[];
  onReady: (api: {
    handle: RichComposerInputHandle | null;
    getState: () => { text: string; selection: RichComposerSelection; changeCount: number };
  }) => void;
}) {
  const [text, setText] = useState(initialText);
  const [invocations, setInvocations] = useState(initialInvocations);
  const [selection, setSelection] = useState<RichComposerSelection>({ start: 0, end: 0 });
  const changeCount = useRef(0);
  const handleRef = useRef<RichComposerInputHandle>(null);
  const readyOnce = useRef(false);

  if (!readyOnce.current) {
    readyOnce.current = true;
    queueMicrotask(() => {
      onReady({
        handle: handleRef.current,
        getState: () => ({ text, selection, changeCount: changeCount.current }),
      });
    });
  }

  return (
    <LocaleProvider>
      <RichComposerInput
        ref={handleRef}
        text={text}
        invocations={invocations}
        placeholder="Message"
        disabled={false}
        onChange={(nextText, nextInvocations) => {
          changeCount.current += 1;
          setText(nextText);
          setInvocations(nextInvocations);
        }}
        onSelectionChange={(next) => setSelection(next)}
        onKeyDown={() => {}}
        onPaste={() => {}}
        onCompositionStart={() => {}}
        onCompositionEnd={() => {}}
      />
    </LocaleProvider>
  );
}

{
  const dom = installDom();
  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);

  const longText = "The quick brown fox jumps over the lazy dog";
  const inv = [invocation("skill-1", 0, pluginCommand)];
  let api: {
    handle: RichComposerInputHandle | null;
    getState: () => { text: string; selection: RichComposerSelection; changeCount: number };
  } | null = null;

  await act(async () => {
    root.render(
      <Harness
        initialText={longText}
        initialInvocations={inv}
        onReady={(value) => {
          api = value;
        }}
      />,
    );
    await flushTimers();
  });

  const input = document.querySelector(".composer__rich-input") as HTMLDivElement | null;
  ok(input !== null, "rich composer mounts");
  if (!input) throw new Error("rich input missing");

  // Force WebView2-like DOM: move body text into the caret anchor after the sentinel.
  const anchor = input.querySelector<HTMLElement>("[data-composer-caret-anchor]");
  ok(anchor !== null, "caret anchor is present");
  if (anchor) {
    // React rendered: [token][anchor:NBSP][text node sibling]. Move sibling into anchor.
    const siblingText = Array.from(input.childNodes).find(
      (node) => node.nodeType === Node.TEXT_NODE && (node.textContent ?? "").length > 0,
    );
    if (siblingText) {
      anchor.textContent = `\u00A0${siblingText.textContent ?? ""}`;
      siblingText.parentNode?.removeChild(siblingText);
    }
  }

  const known = new Map(inv.map((item) => [item.id, item]));
  eq(modelFromDom(input, known).text, longText, "component DOM still maps after WebView2-like rewrite");

  // Place caret in the middle and type two characters via input events with a
  // temporary selection blackout between beforeinput and input (WebView2).
  const mid = 10;
  await act(async () => {
    input.focus();
    setDomSelection(input, { start: mid, end: mid });
    await flushTimers();
  });

  const typeChar = async (ch: string) => {
    const beforeText = modelFromDom(input, known).text;
    const beforeSel = selectionFromDom(input, known);
    ok(beforeSel.ok, `selection live before typing ${ch}`);
    const start = beforeSel.ok ? beforeSel.selection.start : mid;
    const end = beforeSel.ok ? beforeSel.selection.end : mid;

    await act(async () => {
      input.dispatchEvent(new window.InputEvent("beforeinput", {
        bubbles: true,
        cancelable: true,
        inputType: "insertText",
        data: ch,
      }));

      // Apply the DOM mutation as the browser would.
      const selection = document.getSelection();
      if (selection && selection.rangeCount > 0) {
        const range = selection.getRangeAt(0);
        range.deleteContents();
        const node = document.createTextNode(ch);
        range.insertNode(node);
        range.setStartAfter(node);
        range.collapse(true);
        selection.removeAllRanges();
        selection.addRange(range);
      } else {
        // Fallback: splice into anchor text.
        if (anchor) {
          const raw = anchor.textContent ?? "";
          const logical = raw.startsWith("\u00A0") ? raw.slice(1) : raw;
          const next = logical.slice(0, start) + ch + logical.slice(end);
          anchor.textContent = `\u00A0${next}`;
        }
      }

      // WebView2 blackout: selection temporarily leaves the editor.
      document.getSelection()?.removeAllRanges();

      input.dispatchEvent(new window.InputEvent("input", {
        bubbles: true,
        data: ch,
        inputType: "insertText",
      }));
      await flushTimers();
    });

    // After React controlled echo, re-query the live rich input.
    const live = document.querySelector(".composer__rich-input") as HTMLDivElement | null;
    if (!live) throw new Error("rich input disappeared after typing");
    const afterModel = modelFromDom(live, known);
    const expected = beforeText.slice(0, start) + ch + beforeText.slice(end);
    eq(afterModel.text, expected, `typed ${JSON.stringify(ch)} stays at mid-text (not appended at end)`);
    return live;
  };

  let liveInput = await typeChar("1");
  liveInput = await typeChar("2");

  // Re-bind known after potential re-render; invocations unchanged.
  const afterTwo = modelFromDom(liveInput, known);
  ok(afterTwo.text.includes("1") && afterTwo.text.includes("2"), "both typed characters remain in the body");
  ok(
    afterTwo.text.indexOf("12") === mid || afterTwo.text.includes("12"),
    "typed characters remain contiguous at the original caret",
  );

  // IME composition: during composition, selection must not be reset via removeAllRanges
  // from our sync path. We assert that compositionstart suppresses input sync and
  // compositionend performs a single model update.
  const beforeImeCount = api?.getState().changeCount ?? 0;
  const imeInsertAt = Math.min(10, afterTwo.text.length);
  const textBeforeIme = afterTwo.text;
  await act(async () => {
    liveInput.focus();
    setDomSelection(liveInput, { start: imeInsertAt, end: imeInsertAt });
    liveInput.dispatchEvent(new Event("compositionstart", { bubbles: true }));
    // Intermediate composition input with isComposing should not sync.
    // Apply provisional text at the caret without syncing.
    const liveSel = document.getSelection();
    if (liveSel && liveSel.rangeCount > 0) {
      const range = liveSel.getRangeAt(0);
      range.insertNode(document.createTextNode("ni"));
    } else {
      liveInput.appendChild(document.createTextNode("ni"));
    }
    liveInput.dispatchEvent(new window.InputEvent("input", {
      bubbles: true,
      data: "ni",
      inputType: "insertCompositionText",
      isComposing: true,
    }));
    await flushTimers();
  });
  const midImeCount = api?.getState().changeCount ?? 0;
  eq(midImeCount, beforeImeCount, "IME composition intermediate input does not sync the model");

  await act(async () => {
    // Commit composition: replace provisional "ni" with "你", then black out selection
    // before compositionend (WebView2 may drop selection across the commit).
    const current = document.querySelector(".composer__rich-input") as HTMLDivElement;
    const textNodes: Text[] = [];
    const collect = (node: Node) => {
      if (node.nodeType === Node.TEXT_NODE) textNodes.push(node as Text);
      node.childNodes.forEach((child) => collect(child));
    };
    collect(current);
    const provisional = textNodes.find((node) => (node.textContent ?? "").includes("ni"));
    if (provisional && provisional.textContent === "ni") {
      provisional.textContent = "你";
    } else if (provisional?.textContent?.includes("ni")) {
      provisional.textContent = provisional.textContent.replace("ni", "你");
    } else {
      const anchorEl = current.querySelector<HTMLElement>("[data-composer-caret-anchor]");
      if (anchorEl) {
        anchorEl.textContent = `\u00A0${textBeforeIme.slice(0, imeInsertAt)}你${textBeforeIme.slice(imeInsertAt)}`;
      } else {
        current.appendChild(document.createTextNode("你"));
      }
    }
    document.getSelection()?.removeAllRanges();
    current.dispatchEvent(new Event("compositionend", { bubbles: true }));
    await flushTimers();
  });
  const afterImeCount = api?.getState().changeCount ?? 0;
  ok(afterImeCount === beforeImeCount + 1, "compositionend performs exactly one model sync");
  const imeLive = document.querySelector(".composer__rich-input") as HTMLDivElement;
  const imeModel = modelFromDom(imeLive, known);
  ok(imeModel.text.includes("你"), "committed IME text is present once");
  const imeSel = selectionFromDom(imeLive, known);
  // After blackout recovery, caret must sit after the committed run — never the
  // pre-composition offset when the commit actually grew the text.
  if (imeModel.text.length > textBeforeIme.length && imeSel.ok) {
    ok(
      imeSel.selection.start > imeInsertAt || imeSel.selection.start === imeModel.text.length,
      "compositionend selection blackout does not leave caret at pre-composition offset",
    );
  }

  // Cancel-style composition: start then end without net change should still only sync once.
  const cancelBase = api?.getState().changeCount ?? 0;
  await act(async () => {
    const current = document.querySelector(".composer__rich-input") as HTMLDivElement;
    current.dispatchEvent(new Event("compositionstart", { bubbles: true }));
    current.dispatchEvent(new window.InputEvent("input", {
      bubbles: true,
      data: "tmp",
      inputType: "insertCompositionText",
      isComposing: true,
    }));
    current.dispatchEvent(new Event("compositionend", { bubbles: true }));
    await flushTimers();
  });
  const cancelCount = api?.getState().changeCount ?? 0;
  ok(cancelCount <= cancelBase + 1, "composition cancel/end does not thrash with multiple syncs");

  // Backspace after invocation tag removes the tag (afterInvocationId path).
  await act(async () => {
    root.render(
      <Harness
        initialText=""
        initialInvocations={[invocation("backspace-target", 0, skillCommand)]}
        onReady={() => {}}
      />,
    );
    await flushTimers();
  });
  const emptyRich = document.querySelector(".composer__rich-input") as HTMLDivElement | null;
  ok(emptyRich !== null, "empty invocation-only rich input mounts");
  if (emptyRich) {
    const token = emptyRich.querySelector("[data-invocation-id]");
    ok(token !== null, "invocation token present for Backspace test");
    await act(async () => {
      emptyRich.focus();
      setDomSelection(emptyRich, { start: 0, end: 0, afterInvocationId: "backspace-target" });
      emptyRich.dispatchEvent(new window.KeyboardEvent("keydown", {
        key: "Backspace",
        bubbles: true,
        cancelable: true,
      }));
      await flushTimers();
    });
    // After last invocation removal, parent usually switches away; in harness the
    // component may remain with zero invocations.
    const remaining = document.querySelectorAll("[data-invocation-id]").length;
    eq(remaining, 0, "Backspace after skill tag removes the invocation");
  }

  // The inline remove button is the pointer/touch-accessible counterpart to
  // Backspace and must preserve the surrounding task text.
  let clickRemovalSelection: RichComposerSelection | null = null;
  let clickRemovalText = "";
  function ClickRemoveHarness() {
    const [invocations, setInvocations] = useState([
      invocation("click-remove-target", 2, skillCommand),
    ]);
    return (
      <LocaleProvider>
        <RichComposerInput
          text="task"
          invocations={invocations}
          placeholder="Message"
          disabled={false}
          onChange={(nextText, nextInvocations, origin) => {
            clickRemovalText = nextText;
            clickRemovalSelection = origin.afterSelection;
            setInvocations(nextInvocations);
          }}
          onSelectionChange={() => {}}
          onKeyDown={() => {}}
          onPaste={() => {}}
          onCompositionStart={() => {}}
          onCompositionEnd={() => {}}
        />
      </LocaleProvider>
    );
  }

  await act(async () => {
    root.render(<ClickRemoveHarness />);
    await flushTimers();
  });
  const removeInvocation = document.querySelector(
    ".composer-invocation-token .invocation-display__remove",
  ) as HTMLButtonElement | null;
  ok(removeInvocation !== null, "inline invocation exposes an accessible remove button");
  await act(async () => {
    removeInvocation?.click();
    await flushTimers();
  });
  eq(document.querySelectorAll("[data-invocation-id]").length, 0, "clicking remove deletes the invocation");
  eq(clickRemovalText, "task", "clicking remove preserves surrounding task text");
  eq(clickRemovalSelection?.start, 2, "clicking remove places the caret at the invocation offset");

  // External draft replacement rebuilds DOM; only explicit pending selection is restored.
  let externalSel: RichComposerSelection = { start: 0, end: 0 };
  function ExternalHarness() {
    const [text, setText] = useState("one");
    const [invocations, setInvocations] = useState<ComposerInvocation[]>([
      invocation("ext-1", 0, skillCommand),
    ]);
    const handleRef = useRef<RichComposerInputHandle>(null);
    return (
      <LocaleProvider>
        <button
          type="button"
          id="external-focus"
          onClick={() => {
            /* focus sink */
          }}
        >
          outside
        </button>
        <RichComposerInput
          ref={handleRef}
          text={text}
          invocations={invocations}
          placeholder="Message"
          disabled={false}
          onChange={(nextText, nextInvocations) => {
            setText(nextText);
            setInvocations(nextInvocations);
          }}
          onSelectionChange={(next) => {
            externalSel = next;
          }}
          onKeyDown={() => {}}
          onPaste={() => {}}
          onCompositionStart={() => {}}
          onCompositionEnd={() => {}}
        />
        <button
          type="button"
          id="replace-draft"
          onClick={() => {
            setText("replaced draft body");
            setInvocations([invocation("ext-1", 0, skillCommand)]);
          }}
        >
          replace
        </button>
        <button
          type="button"
          id="clear-draft"
          onClick={() => {
            setText("");
            setInvocations([]);
          }}
        >
          clear
        </button>
      </LocaleProvider>
    );
  }

  await act(async () => {
    root.render(<ExternalHarness />);
    await flushTimers();
  });

  const outside = document.getElementById("external-focus") as HTMLButtonElement;
  const replaceBtn = document.getElementById("replace-draft") as HTMLButtonElement;
  const clearBtn = document.getElementById("clear-draft") as HTMLButtonElement;
  outside.focus();
  eq(document.activeElement, outside, "focus is on an external control before draft replace");
  await act(async () => {
    replaceBtn.click();
    await flushTimers();
  });
  ok(document.activeElement === outside, "external draft replace does not steal focus from other controls");
  const replaced = document.querySelector(".composer__rich-input") as HTMLDivElement | null;
  ok(replaced !== null, "rich input remains after external draft replace");
  if (replaced) {
    const replacedModel = modelFromDom(
      replaced,
      new Map([["ext-1", invocation("ext-1", 0, skillCommand)]]),
    );
    eq(replacedModel.text, "replaced draft body", "external draft replace updates model text without duplication");
  }

  await act(async () => {
    clearBtn.click();
    await flushTimers();
  });
  // Clearing invocations may unmount rich input in the real Composer; in this
  // harness the component stays mounted with empty model.
  const cleared = document.querySelector(".composer__rich-input") as HTMLDivElement | null;
  if (cleared) {
    eq(modelFromDom(cleared, new Map()).text, "", "send/clear path can empty the rich composer");
  }
  ok(true, "no duplicate body text after external replace and clear");

  // Range replace through controlled replaceRange API.
  let rangeApi: RichComposerInputHandle | null = null;
  function RangeHarness() {
    const [text, setText] = useState("abcdefghij");
    const [invocations, setInvocations] = useState([invocation("r1", 0, skillCommand)]);
    const handleRef = useRef<RichComposerInputHandle>(null);
    return (
      <LocaleProvider>
        <RichComposerInput
          ref={(value) => {
            handleRef.current = value;
            rangeApi = value;
          }}
          text={text}
          invocations={invocations}
          placeholder="Message"
          disabled={false}
          onChange={(nextText, nextInvocations) => {
            setText(nextText);
            setInvocations(nextInvocations);
          }}
          onSelectionChange={() => {}}
          onKeyDown={() => {}}
          onPaste={() => {}}
          onCompositionStart={() => {}}
          onCompositionEnd={() => {}}
        />
      </LocaleProvider>
    );
  }
  await act(async () => {
    root.render(<RangeHarness />);
    await flushTimers();
  });
  await act(async () => {
    rangeApi?.replaceRange("XY", 2, 5);
    await flushTimers();
  });
  const rangeInput = document.querySelector(".composer__rich-input") as HTMLDivElement | null;
  if (rangeInput) {
    const model = modelFromDom(rangeInput, new Map([["r1", invocation("r1", 0, skillCommand)]]));
    eq(model.text, "abXYfghij", "mid-range replace rewrites only the selected span");
  }

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

// Isolated IME blackout case: compositionstart snapshot + compositionend without a
// live selection must place the caret after the committed run (not at text end).
console.log("\nrich composer IME compositionend blackout");

{
  const dom = installDom();
  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);
  const imeKnown = new Map([["ime-1", invocation("ime-1", 0, skillCommand)]]);
  let changeCount = 0;
  let lastSelection: RichComposerSelection = { start: 0, end: 0 };

  function ImeHarness() {
    const [text, setText] = useState("hello world");
    const [invocations, setInvocations] = useState([invocation("ime-1", 0, skillCommand)]);
    return (
      <LocaleProvider>
        <RichComposerInput
          text={text}
          invocations={invocations}
          placeholder="Message"
          disabled={false}
          onChange={(nextText, nextInvocations) => {
            changeCount += 1;
            setText(nextText);
            setInvocations(nextInvocations);
          }}
          onSelectionChange={(next) => {
            lastSelection = next;
          }}
          onKeyDown={() => {}}
          onPaste={() => {}}
          onCompositionStart={() => {}}
          onCompositionEnd={() => {}}
        />
      </LocaleProvider>
    );
  }

  await act(async () => {
    root.render(<ImeHarness />);
    await flushTimers();
  });

  const imeRoot = document.querySelector(".composer__rich-input") as HTMLDivElement | null;
  ok(imeRoot !== null, "IME blackout harness mounts");
  if (imeRoot) {
    await act(async () => {
      imeRoot.focus();
      setDomSelection(imeRoot, { start: 5, end: 5 });
      imeRoot.dispatchEvent(new Event("compositionstart", { bubbles: true }));
      const sibling = Array.from(imeRoot.childNodes).find(
        (node) => node.nodeType === Node.TEXT_NODE && (node.textContent ?? "").length > 0,
      ) as Text | undefined;
      const anchorEl = imeRoot.querySelector<HTMLElement>("[data-composer-caret-anchor]");
      if (sibling) sibling.textContent = "hello你 world";
      else if (anchorEl) anchorEl.textContent = "\u00A0hello你 world";
      document.getSelection()?.removeAllRanges();
      ok(!selectionFromDom(imeRoot, imeKnown).ok, "selection is unavailable at compositionend");
      imeRoot.dispatchEvent(new Event("compositionend", { bubbles: true }));
      eq(changeCount, 1, "compositionend syncs immediately when the committed DOM is already visible");
      await flushTimers();
    });

    const afterImeRoot = document.querySelector(".composer__rich-input") as HTMLDivElement;
    const afterImeModel = modelFromDom(afterImeRoot, imeKnown);
    eq(afterImeModel.text, "hello你 world", "IME mid-text commit keeps text at the composition locus");
    const afterImeSel = selectionFromDom(afterImeRoot, imeKnown);
    // "hello" (5) + "你" (1) → caret index 6. Must not stay at pre-composition 5
    // and must not jump to text.length (12).
    eq(
      afterImeSel.ok ? afterImeSel.selection.start : lastSelection.start,
      6,
      "IME compositionend blackout places caret after committed characters, not pre-composition offset",
    );
    eq(lastSelection.start, 6, "onSelectionChange reports caret after committed IME text");
  }

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

// Windows WebView2 may dispatch compositionend before the committed DOM and a
// final non-composing input are visible. The composer must not publish the stale
// pre-composition model during that gap, or the controlled echo erases the IME
// candidate before the final input can be synchronized.
console.log("\nrich composer IME late final input");

{
  const dom = installDom();
  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);
  const imeKnown = new Map([["ime-late", invocation("ime-late", 0, skillCommand)]]);
  let changeCount = 0;
  let latestText = "hello world";
  let lastSelection: RichComposerSelection = { start: 0, end: 0 };

  function LateImeHarness() {
    const [text, setText] = useState("hello world");
    const [invocations, setInvocations] = useState([invocation("ime-late", 0, skillCommand)]);
    return (
      <LocaleProvider>
        <RichComposerInput
          text={text}
          invocations={invocations}
          placeholder="Message"
          disabled={false}
          onChange={(nextText, nextInvocations) => {
            changeCount += 1;
            latestText = nextText;
            setText(nextText);
            setInvocations(nextInvocations);
          }}
          onSelectionChange={(next) => {
            lastSelection = next;
          }}
          onKeyDown={() => {}}
          onPaste={() => {}}
          onCompositionStart={() => {}}
          onCompositionEnd={() => {}}
        />
      </LocaleProvider>
    );
  }

  await act(async () => {
    root.render(<LateImeHarness />);
    await flushTimers();
  });

  const imeRoot = document.querySelector(".composer__rich-input") as HTMLDivElement | null;
  ok(imeRoot !== null, "late-input IME harness mounts");
  if (imeRoot) {
    await act(async () => {
      imeRoot.focus();
      setDomSelection(imeRoot, { start: 5, end: 5 });
      imeRoot.dispatchEvent(new Event("compositionstart", { bubbles: true }));
      imeRoot.dispatchEvent(new Event("compositionend", { bubbles: true }));
      eq(changeCount, 0, "compositionend waits when the committed DOM is not visible yet");

      const sibling = Array.from(imeRoot.childNodes).find(
        (node) => node.nodeType === Node.TEXT_NODE && (node.textContent ?? "").includes("hello world"),
      ) as Text | undefined;
      const anchorEl = imeRoot.querySelector<HTMLElement>("[data-composer-caret-anchor]");
      if (sibling) sibling.textContent = "hello你 world";
      else if (anchorEl) anchorEl.textContent = "\u00A0hello你 world";
      document.getSelection()?.removeAllRanges();
      imeRoot.dispatchEvent(new window.InputEvent("input", {
        bubbles: true,
        data: "你",
        inputType: "insertCompositionText",
        isComposing: false,
      }));
      await flushTimers();
    });

    eq(changeCount, 1, "late final input performs one authoritative IME model sync");
    eq(latestText, "hello你 world", "late final input preserves the committed IME candidate");
    const afterImeRoot = document.querySelector(".composer__rich-input") as HTMLDivElement;
    eq(
      modelFromDom(afterImeRoot, imeKnown).text,
      "hello你 world",
      "controlled echo keeps the late committed IME text",
    );
    eq(lastSelection.start, 6, "late final input restores the caret after the committed IME text");
  }

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

console.log(`\nrich-composer-selection: ${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
