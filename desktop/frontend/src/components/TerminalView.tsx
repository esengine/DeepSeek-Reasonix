import { useEffect, useRef } from "react";
import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";

import { useTerminalStore } from "../store/terminal";
import { registerTerminalSink, startTerminalEventBridge } from "../lib/terminalEvents";
import { observeTerminalTheme, terminalThemeForElement } from "../lib/terminalTheme";
import { detectShortcutPlatform, matchesShortcut } from "../lib/keyboardShortcuts";
import type { TerminalSessionView } from "../lib/types";

export function TerminalView({ tabId, session }: { tabId: string; session: TerminalSessionView }) {
  const hostRef = useRef<HTMLDivElement>(null);
  const write = useTerminalStore((state) => state.write);
  const resize = useTerminalStore((state) => state.resize);

  useEffect(() => {
    startTerminalEventBridge();
    const host = hostRef.current;
    if (!host) return;
    const terminal = new Terminal({
      convertEol: true,
      cursorBlink: true,
      fontFamily: "ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace",
      fontSize: 13,
      theme: terminalThemeForElement(host),
    });
    const fit = new FitAddon();
    terminal.loadAddon(fit);
    terminal.open(host);
    const updateTheme = () => {
      terminal.options.theme = terminalThemeForElement(host);
    };
    const stopObservingTheme = observeTerminalTheme(host, updateTheme);
    const unregister = registerTerminalSink(session.id, (bytes) => terminal.write(bytes));
    const input = terminal.onData((data) => { void write(tabId, session.id, data).catch(() => {}); });
    const outputResize = terminal.onResize(({ cols, rows }) => { void resize(tabId, session.id, cols, rows).catch(() => {}); });
    // Clear-screen-and-scrollback lives in the shared shortcut registry as
    // "terminal.clear" (Cmd+K on macOS, Ctrl+K elsewhere — VSCode's terminal
    // convention), so users can rebind or inspect it from the shortcut
    // settings like any other action. xterm.clear() empties the whole buffer,
    // making the prompt line the new first line. The capture-phase listener
    // fires before xterm's own key handling, and only while focus is anywhere
    // inside the terminal panel (not just the xterm element), so the chord
    // keeps its native meaning in the composer and other editors.
    const clearTerminal = (event: globalThis.KeyboardEvent) => {
      if (!(event.target instanceof Node)) return;
      const panel = host.closest(".terminal-panel");
      if (!panel || !panel.contains(event.target)) return;
      if (!matchesShortcut(event, "terminal.clear", detectShortcutPlatform())) return;
      event.preventDefault();
      event.stopImmediatePropagation();
      terminal.clear();
    };
    document.addEventListener("keydown", clearTerminal, { capture: true });
    const fitTerminal = () => {
      fit.fit();
      const { cols, rows } = terminal;
      if (cols > 0 && rows > 0) void resize(tabId, session.id, cols, rows).catch(() => {});
    };
    const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(fitTerminal);
    observer?.observe(host);
    fitTerminal();
    return () => {
      observer?.disconnect();
      stopObservingTheme();
      document.removeEventListener("keydown", clearTerminal, { capture: true });
      input.dispose();
      outputResize.dispose();
      unregister();
      terminal.dispose();
    };
  }, [resize, session.id, tabId, write]);

  return <div ref={hostRef} className="terminal-view" aria-label={session.title} />;
}
