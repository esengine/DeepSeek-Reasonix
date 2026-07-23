// Run: tsx src/__tests__/transcript-selection-menu.test.tsx
//
// Regression coverage for transcript selection actions. Selected message text
// exposes Add to Chat after pointer/keyboard selection, while the Wails shell
// also keeps its app-drawn right-click Copy menu:
// - a non-collapsed selection inside .msg__body opens the menu and Copy
//   writes the selection through the runtime clipboard bridge
// - collapsed selections, non-message selections, editable targets, and
//   plain-browser sessions (no window.runtime) never open the menu
// - a surviving message selection does not hijack right-clicks landing
//   outside message bodies (project tree, tab bar, ... own those menus)
// - the target message must itself touch the selection: selecting message A
//   and right-clicking message B offers nothing (Copy would copy A), while a
//   selection spanning both accepts a right-click on either
// - Escape dismisses the floating action without clearing the selection, the
//   trailing keyup does not re-open it, and a fresh pointer gesture does
// - the add-to-chat shortcut lives in the shared registry: rebinding it in
//   settings remaps both the handler and the visible hint

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { TranscriptSelectionMenu } from "../components/TranscriptSelectionMenu";
import { LocaleProvider } from "../lib/i18n";
import { resetCustomShortcuts, saveCustomShortcut } from "../lib/keyboardShortcuts";

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

function flushTimers(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

async function drainFrame(): Promise<void> {
  await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
  await flushTimers();
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
  globalThis.Element = dom.window.Element;
  globalThis.HTMLElement = dom.window.HTMLElement;
  globalThis.HTMLTextAreaElement = dom.window.HTMLTextAreaElement;
  globalThis.Event = dom.window.Event;
  globalThis.CustomEvent = dom.window.CustomEvent;
  globalThis.KeyboardEvent = dom.window.KeyboardEvent;
  globalThis.MouseEvent = dom.window.MouseEvent;
  globalThis.PointerEvent = dom.window.MouseEvent as unknown as typeof PointerEvent;
  globalThis.MutationObserver = dom.window.MutationObserver;
  globalThis.localStorage = dom.window.localStorage;
  globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
  globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
  return dom;
}

function selectNodeText(node: Node) {
  const range = document.createRange();
  range.selectNodeContents(node);
  const selection = document.getSelection();
  selection?.removeAllRanges();
  selection?.addRange(range);
}

async function dispatchContextMenu(target: Element, clientX = 120, clientY = 80): Promise<MouseEvent> {
  const event = new window.MouseEvent("contextmenu", { bubbles: true, cancelable: true, clientX, clientY });
  await act(async () => {
    target.dispatchEvent(event);
    await flushTimers();
  });
  return event;
}

console.log("\ntranscript selection menu");

{
  const dom = installDom();
  const clipboard: string[] = [];
  const additions: string[] = [];
  (window as unknown as { runtime: { ClipboardSetText: (text: string) => Promise<boolean> } }).runtime = {
    ClipboardSetText: async (text: string) => {
      clipboard.push(text);
      return true;
    },
  };

  document.body.insertAdjacentHTML(
    "beforeend",
    "<div class=\"msg__body\">assistant reply text</div>" +
      "<div class=\"msg__body\" id=\"second-message\">second reply text</div>" +
      "<p id=\"plain\">plain page text</p>" +
      "<div id=\"sidebar\">project tree area</div>" +
      "<textarea id=\"editor\"></textarea>",
  );
  const msgBody = document.querySelector(".msg__body") as HTMLElement;
  const secondMsg = document.querySelector("#second-message") as HTMLElement;
  const plain = document.querySelector("#plain") as HTMLElement;
  const sidebar = document.querySelector("#sidebar") as HTMLElement;
  const editor = document.querySelector("#editor") as HTMLTextAreaElement;

  const root = createRoot(document.getElementById("root") as HTMLElement);
  await act(async () => {
    root.render(
      <LocaleProvider>
        <TranscriptSelectionMenu onAddToChat={(text) => additions.push(text)} />
      </LocaleProvider>,
    );
    await flushTimers();
  });

  // Message selection opens the menu and suppresses the (already dead) default.
  selectNodeText(msgBody.firstChild as Node);
  const openEvent = await dispatchContextMenu(msgBody);
  eq(openEvent.defaultPrevented, true, "message selection right-click is claimed by the app menu");
  const menu = document.querySelector(".context-menu");
  ok(menu != null, "message selection right-click opens the transcript menu");
  const copyItem = menu?.querySelector("[role=\"menuitem\"]") as HTMLButtonElement | null;
  eq(copyItem?.textContent?.includes("Copy"), true, "transcript menu offers Copy");

  await act(async () => {
    copyItem?.click();
    await flushTimers();
  });
  eq(clipboard[0], "assistant reply text", "Copy writes the selection through the clipboard bridge");
  eq(document.querySelector(".context-menu"), null, "transcript menu closes after Copy");

  // Releasing a pointer after selecting message text exposes the compact Add
  // to Chat action. It adds the exact selection, clears the browser highlight,
  // and closes without sending anything itself.
  selectNodeText(msgBody.firstChild as Node);
  await act(async () => {
    msgBody.dispatchEvent(new window.MouseEvent("pointerup", { bubbles: true, button: 0 }));
    await drainFrame();
  });
  const addButton = document.querySelector(".transcript-selection-action button") as HTMLButtonElement | null;
  eq(addButton?.textContent?.includes("Add to Chat"), true, "pointer selection exposes Add to Chat");
  await act(async () => {
    addButton?.click();
    await flushTimers();
  });
  eq(additions[0], "assistant reply text", "Add to Chat forwards the exact selected text");
  eq(document.getSelection()?.isCollapsed, true, "Add to Chat clears the browser selection");
  eq(document.querySelector(".transcript-selection-action"), null, "Add to Chat closes the floating action");

  // The shortcut is scoped to a live transcript selection, so it cannot steal
  // Cmd/Ctrl+L during normal app navigation.
  selectNodeText(msgBody.firstChild as Node);
  await act(async () => {
    msgBody.dispatchEvent(new window.MouseEvent("pointerup", { bubbles: true, button: 0 }));
    await drainFrame();
  });
  await act(async () => {
    document.dispatchEvent(new window.KeyboardEvent("keydown", { key: "l", ctrlKey: true, bubbles: true, cancelable: true }));
    await flushTimers();
  });
  eq(additions[1], "assistant reply text", "Cmd/Ctrl+L adds the active transcript selection");

  // Escape dismisses the floating action while the browser selection survives;
  // the trailing keyup must not re-open it, but a fresh pointer gesture does.
  selectNodeText(msgBody.firstChild as Node);
  await act(async () => {
    msgBody.dispatchEvent(new window.MouseEvent("pointerup", { bubbles: true, button: 0 }));
    await drainFrame();
  });
  ok(document.querySelector(".transcript-selection-action") != null, "pointer selection re-exposes the floating action");
  await act(async () => {
    document.dispatchEvent(new window.KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
    await flushTimers();
  });
  eq(document.querySelector(".transcript-selection-action"), null, "Escape dismisses the floating action");
  eq(document.getSelection()?.isCollapsed, false, "Escape keeps the browser selection");
  await act(async () => {
    document.dispatchEvent(new window.KeyboardEvent("keyup", { key: "Escape", bubbles: true }));
    await drainFrame();
  });
  eq(document.querySelector(".transcript-selection-action"), null, "the Escape keyup does not re-open the dismissed action");
  await act(async () => {
    msgBody.dispatchEvent(new window.MouseEvent("pointerup", { bubbles: true, button: 0 }));
    await drainFrame();
  });
  ok(document.querySelector(".transcript-selection-action") != null, "a fresh pointer gesture re-opens the dismissed action");

  // Rebinding selection.addToChat through the shared shortcut registry remaps
  // both the handler and the visible hint; the old combo stops firing.
  await act(async () => {
    saveCustomShortcut("selection.addToChat", { key: "m", ctrl: true });
    await flushTimers();
  });
  eq(
    document.querySelector(".transcript-selection-action kbd")?.textContent,
    "Ctrl+M",
    "the floating action hint tracks the rebound shortcut",
  );
  await act(async () => {
    document.dispatchEvent(new window.KeyboardEvent("keydown", { key: "l", ctrlKey: true, bubbles: true, cancelable: true }));
    await flushTimers();
  });
  eq(additions.length, 2, "the old combo no longer fires after a rebind");
  await act(async () => {
    document.dispatchEvent(new window.KeyboardEvent("keydown", { key: "m", ctrlKey: true, bubbles: true, cancelable: true }));
    await flushTimers();
  });
  eq(additions[2], "assistant reply text", "the rebound combo adds the selection");
  await act(async () => {
    resetCustomShortcuts();
    await flushTimers();
  });

  // A session/tab switch must discard the captured selection: the overlay only
  // stores text while onAddToChat routes to the tab active at click time, so a
  // surviving overlay could add session A's selection to session B. During
  // placeholder hydration the previous transcript stays on screen, so the
  // disabled window must also keep the shortcut keyup from re-summoning it.
  selectNodeText(msgBody.firstChild as Node);
  await act(async () => {
    msgBody.dispatchEvent(new window.MouseEvent("pointerup", { bubbles: true, button: 0 }));
    await drainFrame();
  });
  ok(document.querySelector(".transcript-selection-action") != null, "selection opens the floating action before a tab switch");
  await act(async () => {
    root.render(
      <LocaleProvider>
        <TranscriptSelectionMenu onAddToChat={(text) => additions.push(text)} resetKey="tab-b" />
      </LocaleProvider>,
    );
    await flushTimers();
  });
  eq(document.querySelector(".transcript-selection-action"), null, "a tab switch discards the captured selection action");
  await act(async () => {
    root.render(
      <LocaleProvider>
        <TranscriptSelectionMenu onAddToChat={(text) => additions.push(text)} resetKey="tab-b" enabled={false} />
      </LocaleProvider>,
    );
    await flushTimers();
  });
  await act(async () => {
    document.dispatchEvent(new window.KeyboardEvent("keyup", { key: "Meta", bubbles: true }));
    await drainFrame();
  });
  eq(document.querySelector(".transcript-selection-action"), null, "keyup over a hydration placeholder cannot re-summon the action");

  selectNodeText(msgBody.firstChild as Node);
  await act(async () => {
    root.render(
      <LocaleProvider>
        <TranscriptSelectionMenu onAddToChat={(text) => additions.push(text)} resetKey="tab-b" />
      </LocaleProvider>,
    );
    await flushTimers();
  });
  await dispatchContextMenu(msgBody);
  ok(document.querySelector(".context-menu") != null, "the copy menu opens before a tab switch");
  await act(async () => {
    root.render(
      <LocaleProvider>
        <TranscriptSelectionMenu onAddToChat={(text) => additions.push(text)} resetKey="tab-c" />
      </LocaleProvider>,
    );
    await flushTimers();
  });
  eq(document.querySelector(".context-menu"), null, "a tab switch also discards the copy menu");

  // Collapsed selection: no menu, default untouched.
  document.getSelection()?.removeAllRanges();
  const collapsedEvent = await dispatchContextMenu(msgBody);
  eq(collapsedEvent.defaultPrevented, false, "collapsed selection leaves the event alone");
  eq(document.querySelector(".context-menu"), null, "collapsed selection does not open the menu");

  // Selection outside any message body: no menu.
  selectNodeText(plain.firstChild as Node);
  await dispatchContextMenu(plain);
  eq(document.querySelector(".context-menu"), null, "non-message selection does not open the menu");

  // Selecting message A and right-clicking message B must offer nothing:
  // Copy would copy A's text, not what sits under the click.
  selectNodeText(msgBody.firstChild as Node);
  const otherMessageEvent = await dispatchContextMenu(secondMsg);
  eq(otherMessageEvent.defaultPrevented, false, "right-click on a message outside the selection leaves the event alone");
  eq(document.querySelector(".context-menu"), null, "selection in message A does not open the menu on message B");

  // A selection spanning both messages accepts a right-click on either.
  {
    const range = document.createRange();
    range.setStartBefore(msgBody.firstChild as Node);
    range.setEndAfter(secondMsg.firstChild as Node);
    const selection = document.getSelection();
    selection?.removeAllRanges();
    selection?.addRange(range);
  }
  await dispatchContextMenu(secondMsg);
  ok(document.querySelector(".context-menu") != null, "cross-message selection opens the menu on either message");
  await act(async () => {
    window.dispatchEvent(new window.KeyboardEvent("keydown", { key: "Escape" }));
    await flushTimers();
  });

  // A surviving message selection must not hijack right-clicks landing outside
  // message bodies — the project tree, tab bar, etc. own those context menus.
  selectNodeText(msgBody.firstChild as Node);
  const sidebarEvent = await dispatchContextMenu(sidebar);
  eq(sidebarEvent.defaultPrevented, false, "right-click outside message bodies leaves the event alone");
  eq(document.querySelector(".context-menu"), null, "message selection does not open the menu over other surfaces");

  // Editable target keeps its native menu even while a message selection exists.
  selectNodeText(msgBody.firstChild as Node);
  const editableEvent = await dispatchContextMenu(editor);
  eq(editableEvent.defaultPrevented, false, "editable targets keep the native menu");
  eq(document.querySelector(".context-menu"), null, "editable targets do not open the transcript menu");

  // Keyboard menu key fires at (0,0); the menu still opens, anchored to the selection.
  selectNodeText(msgBody.firstChild as Node);
  await dispatchContextMenu(msgBody, 0, 0);
  ok(document.querySelector(".context-menu") != null, "keyboard-invoked menu opens without pointer coordinates");
  await act(async () => {
    window.dispatchEvent(new window.KeyboardEvent("keydown", { key: "Escape" }));
    await flushTimers();
  });
  eq(document.querySelector(".context-menu"), null, "Escape closes the transcript menu");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  // Plain browser (no window.runtime): the native menu owns right-click.
  const dom = installDom();
  document.body.insertAdjacentHTML("beforeend", "<div class=\"msg__body\">browser text</div>");
  const msgBody = document.querySelector(".msg__body") as HTMLElement;

  const root = createRoot(document.getElementById("root") as HTMLElement);
  await act(async () => {
    root.render(
      <LocaleProvider>
        <TranscriptSelectionMenu />
      </LocaleProvider>,
    );
    await flushTimers();
  });

  selectNodeText(msgBody.firstChild as Node);
  const browserEvent = await dispatchContextMenu(msgBody);
  eq(browserEvent.defaultPrevented, false, "plain browser keeps the native selection menu");
  eq(document.querySelector(".context-menu"), null, "plain browser never sees the app menu");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
