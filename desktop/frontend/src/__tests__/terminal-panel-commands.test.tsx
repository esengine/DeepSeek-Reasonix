import assert from "node:assert/strict";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import { useTerminalPanelCommands } from "../app-runtime/useTerminalPanelCommands";
import { useLayoutStore } from "../store/layout";
import { useAppNavigationStore } from "../store/appNavigation";
import { useTerminalStore } from "../store/terminal";
import { AppBottomRegions } from "../app-shell/AppBottomRegions";
import { TopicbarSessionActions } from "../components/TopicbarSessionActions";
import { LocaleProvider, useT } from "../lib/i18n";
import { ToastProvider } from "../lib/toast";

const dom = new JSDOM("<div id='root'></div>", { url: "http://localhost" });
Object.assign(globalThis, { window: dom.window, document: dom.window.document, localStorage: dom.window.localStorage,
  KeyboardEvent: dom.window.KeyboardEvent, IS_REACT_ACT_ENVIRONMENT: true });
const root = createRoot(document.getElementById("root")!);
const originalCreate = useTerminalStore.getState().createSession;
const calls: string[] = [];
useTerminalStore.setState({ createSession: async (tab, path) => { calls.push(`${tab}:${path}`); return null; } });
let commands!: ReturnType<typeof useTerminalPanelCommands>;
const noop = () => {};
function Probe({ remote }: { remote: boolean }) {
  const page = useAppNavigationStore(state => state.page);
  commands = useTerminalPanelCommands({ tabId: "A", enabled: !remote, shortcutsEnabled: page.kind === "workspace" });
  const t = useT();
  return <>
    <TopicbarSessionActions sessionHasContent={false} getSessionMarkdown={() => ""} exportSession={noop}
      toggleTerminal={commands.toggleTerminalPanel} terminalEnabled={!remote} terminalOpen={false} openSessionSummary={noop} tasksOpen={false} />
    {remote && <AppBottomRegions terminal={{ open: false, contentVisible: true, remoteSurface: true, t,
      panel: { tabId: "A", open: false, onClose: commands.closeTerminalPanel },
      resizer: { min: 100, max: 400, value: 200, onPointerDown: noop, onKeyDown: noop, onReset: noop },
    }} />}
  </>;
}
const paint = (remote: boolean) => act(async () => root.render(<LocaleProvider><ToastProvider><Probe remote={remote} /></ToastProvider></LocaleProvider>));
const key = (shiftKey = false) => document.dispatchEvent(new KeyboardEvent("keydown", { key: "`", ctrlKey: true, shiftKey, bubbles: true }));
try {
  useLayoutStore.getState().setTerminalPanelOpen(false);
  await paint(true);
  const terminalButton = document.querySelector(".lucide-terminal")?.closest("button")
    ?? [...document.querySelectorAll("button")].find(button => button.getAttribute("aria-label") === "Terminal");
  assert.ok(terminalButton);
  assert.equal(terminalButton.disabled, true);
  assert.equal(document.querySelector(".terminal-drawer")?.childElementCount, 0, "remote surface cannot mount a warm local TerminalPanel");
  await act(async () => { key(); key(true); commands.openTerminalForPath("remote-path"); });
  assert.equal(useLayoutStore.getState().terminalPanelOpen, false);
  assert.deepEqual(calls, [], "remote shortcut and direct commands share one local-tool capability gate");
  await paint(false);
  await act(async () => useAppNavigationStore.getState().openPage({ kind: "automation" }));
  await act(async () => key());
  assert.equal(useAppNavigationStore.getState().page.kind, "automation");
  assert.equal(useLayoutStore.getState().terminalPanelOpen, false, "management pages suppress workspace shortcuts without changing stored geometry");
  await act(async () => useAppNavigationStore.getState().returnToWorkspace());
  await act(async () => key());
  assert.equal(useLayoutStore.getState().terminalPanelOpen, true);
  await act(async () => key(true));
  assert.deepEqual(calls, ["A:."]);
  await act(async () => commands.closeTerminalPanel());
  assert.equal(useLayoutStore.getState().terminalPanelOpen, false);
  await act(async () => root.unmount());
  commands.openTerminalForPath("stale"); key(true);
  assert.deepEqual(calls, ["A:."], "unmount revokes commands and removes shortcut listeners");
  console.log("terminal commands: remote capability, warm mount exclusion, native key routing and disposal passed");
} finally { useTerminalStore.setState({ createSession: originalCreate }); dom.window.close(); }
