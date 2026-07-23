// Run: tsx src/__tests__/shortcuts-recorder-focus.test.tsx
//
// Regression test for the shortcut recorder on WebKit (WKWebView). WebKit does
// not focus <button> elements on mouse click, and the recorder's keydown
// listener lives on the button — so without an explicit focus() the recorder
// never sees any keys. JSDOM clicks share that behavior, which lets this test
// reproduce the WebKit flow exactly.

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";

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

function flushPromises(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
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
  globalThis.HTMLButtonElement = dom.window.HTMLButtonElement;
  globalThis.Event = dom.window.Event;
  globalThis.MouseEvent = dom.window.MouseEvent;
  globalThis.KeyboardEvent = dom.window.KeyboardEvent;
  globalThis.FocusEvent = dom.window.FocusEvent;
  globalThis.CustomEvent = dom.window.CustomEvent;
  globalThis.localStorage = dom.window.localStorage;
  globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
  globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
  Object.defineProperty(window, "matchMedia", {
    configurable: true,
    value: () => ({
      matches: false,
      media: "",
      addEventListener: () => {},
      removeEventListener: () => {},
      addListener: () => {},
      removeListener: () => {},
    }),
  });
  return dom;
}

async function main() {
  installDom();

  // Import after the DOM globals exist so module-level window guards hold.
  const { ShortcutsSection } = await import("../components/SettingsPanel");
  const { LocaleProvider } = await import("../lib/i18n");
  const { loadCustomShortcuts, resetCustomShortcuts } = await import("../lib/keyboardShortcuts");

  resetCustomShortcuts();

  const container = document.getElementById("root")!;
  const root = createRoot(container);
  await act(async () => {
    root.render(
      <LocaleProvider>
        <ShortcutsSection />
      </LocaleProvider>,
    );
    await flushPromises();
  });

  const keyButton = container.querySelector<HTMLButtonElement>(".shortcuts-settings__key");
  ok(Boolean(keyButton), "recorder button renders");
  if (!keyButton) throw new Error("no recorder button");

  // Click WITHOUT focusing — JSDOM (like WebKit/WKWebView) does not focus
  // buttons on click, so this reproduces the desktop app's event flow.
  await act(async () => {
    keyButton.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    await flushPromises();
  });

  ok(keyButton.classList.contains("shortcuts-settings__key--recording"), "clicking enters recording state");
  ok(document.activeElement === keyButton, "recorder button is focused after click (WebKit needs explicit focus)");

  // The key lands on whatever has focus — exactly what WKWebView does.
  await act(async () => {
    (document.activeElement ?? document.body).dispatchEvent(
      new KeyboardEvent("keydown", { key: "Enter", ctrlKey: true, bubbles: true, cancelable: true }),
    );
    await flushPromises();
  });

  ok(!keyButton.classList.contains("shortcuts-settings__key--recording"), "combo press leaves recording state");
  const saved = loadCustomShortcuts();
  const first = Object.values(saved)[0];
  ok(Boolean(first && first.key === "Enter" && first.ctrl), "Ctrl+Enter is saved as the custom combo");

  // Losing focus while recording must cancel the recording state, otherwise
  // the UI claims to listen while no keys can reach it.
  await act(async () => {
    keyButton.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    await flushPromises();
  });
  ok(keyButton.classList.contains("shortcuts-settings__key--recording"), "second click re-enters recording state");
  await act(async () => {
    keyButton.dispatchEvent(new FocusEvent("blur", { bubbles: false }));
    keyButton.dispatchEvent(new FocusEvent("focusout", { bubbles: true }));
    await flushPromises();
  });
  ok(!keyButton.classList.contains("shortcuts-settings__key--recording"), "blur cancels the recording state");

  await act(async () => {
    resetCustomShortcuts();
    await flushPromises();
  });
  const sendButton = container.querySelector<HTMLButtonElement>('[data-shortcut-action="composer.send"]');
  ok(Boolean(sendButton), "composer send recorder renders");
  if (!sendButton) throw new Error("no composer send recorder");

  await act(async () => {
    sendButton.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    await flushPromises();
  });
  await act(async () => {
    sendButton.dispatchEvent(
      new KeyboardEvent("keydown", { key: "s", ctrlKey: true, bubbles: true, cancelable: true }),
    );
    await flushPromises();
  });
  ok(sendButton.classList.contains("shortcuts-settings__key--recording"), "non-Enter key keeps the composer recorder active");
  ok(!loadCustomShortcuts()["composer.send"], "non-Enter key is not saved for composer send");
  ok(Boolean(container.querySelector('[role="alert"]')), "non-Enter key shows an Enter-only validation message");

  await act(async () => {
    sendButton.dispatchEvent(
      new KeyboardEvent("keydown", { key: "Enter", ctrlKey: true, bubbles: true, cancelable: true }),
    );
    await flushPromises();
  });
  const savedSend = loadCustomShortcuts()["composer.send"];
  ok(!sendButton.classList.contains("shortcuts-settings__key--recording"), "Ctrl+Enter completes composer shortcut recording");
  ok(Boolean(savedSend && savedSend.key === "Enter" && savedSend.ctrl), "Ctrl+Enter is saved for composer send");

  await act(async () => {
    sendButton.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    await flushPromises();
  });
  await act(async () => {
    sendButton.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true }));
    await flushPromises();
  });
  ok(!sendButton.classList.contains("shortcuts-settings__key--recording"), "Escape cancels composer shortcut recording");
  const afterEscape = loadCustomShortcuts()["composer.send"];
  ok(Boolean(afterEscape && afterEscape.key === "Enter" && afterEscape.ctrl), "Escape preserves the saved composer shortcut");

  await act(async () => {
    sendButton.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    await flushPromises();
  });
  const tabEvent = new KeyboardEvent("keydown", { key: "Tab", bubbles: true, cancelable: true });
  await act(async () => {
    sendButton.dispatchEvent(tabEvent);
    await flushPromises();
  });
  ok(!tabEvent.defaultPrevented, "Tab remains available for keyboard focus navigation");
  ok(document.activeElement !== sendButton, "Tab releases focus when native traversal is unavailable");
  ok(!sendButton.classList.contains("shortcuts-settings__key--recording"), "Tab exits composer shortcut recording");
  const afterTab = loadCustomShortcuts()["composer.send"];
  ok(Boolean(afterTab && afterTab.key === "Enter" && afterTab.ctrl), "Tab preserves the saved composer shortcut");

  await act(async () => {
    resetCustomShortcuts();
    await flushPromises();
  });
  await act(async () => {
    root.unmount();
  });

  console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
  if (failed > 0) process.exit(1);
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
