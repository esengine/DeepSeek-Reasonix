import { forwardRef, useEffect, useImperativeHandle, useRef } from "react";
import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";

import { useTerminalStore } from "../store/terminal";
import { registerTerminalSink, startTerminalEventBridge } from "../lib/terminalEvents";
import {
  clampTerminalSelectionPointToHost,
  normalizeTerminalSelectionText,
  terminalSelectionPointFromHost,
  type TerminalSelectionPoint,
} from "../lib/terminalSelection";
import { observeTerminalTheme, terminalThemeForElement } from "../lib/terminalTheme";
import type { TerminalSessionView } from "../lib/types";

export type TerminalSelectionAction = {
  text: string;
  point: TerminalSelectionPoint;
};

export type TerminalViewHandle = {
  clearSelection: () => void;
};

export const TerminalView = forwardRef<TerminalViewHandle, {
  tabId: string;
  session: TerminalSessionView;
  // When false, skip fit/PTY resize so the drawer open/close height animation
  // is not competing with xterm layout + backend resize IPC on every frame.
  fitEnabled?: boolean;
  onSelectionActionChange?: (action: TerminalSelectionAction | null) => void;
}>(function TerminalView({ tabId, session, fitEnabled = true, onSelectionActionChange }, ref) {
  const hostRef = useRef<HTMLDivElement>(null);
  const terminalRef = useRef<Terminal | null>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const fitEnabledRef = useRef(fitEnabled);
  const onSelectionActionChangeRef = useRef(onSelectionActionChange);
  const write = useTerminalStore((state) => state.write);
  const resize = useTerminalStore((state) => state.resize);
  fitEnabledRef.current = fitEnabled;
  onSelectionActionChangeRef.current = onSelectionActionChange;

  useImperativeHandle(ref, () => ({
    clearSelection: () => {
      terminalRef.current?.clearSelection();
      onSelectionActionChangeRef.current?.(null);
    },
  }), []);

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
    terminalRef.current = terminal;
    const fit = new FitAddon();
    fitRef.current = fit;
    terminal.loadAddon(fit);
    terminal.open(host);
    const updateTheme = () => {
      terminal.options.theme = terminalThemeForElement(host);
    };
    const stopObservingTheme = observeTerminalTheme(host, updateTheme);
    const unregister = registerTerminalSink(session.id, (bytes) => terminal.write(bytes));
    const input = terminal.onData((data) => { void write(tabId, session.id, data).catch(() => {}); });
    const outputResize = terminal.onResize(({ cols, rows }) => { void resize(tabId, session.id, cols, rows).catch(() => {}); });

    const clearSelectionAction = () => {
      onSelectionActionChangeRef.current?.(null);
    };
    const reportSelection = (fallbackPoint?: TerminalSelectionPoint) => {
      const text = normalizeTerminalSelectionText(terminal.getSelection());
      if (!text) {
        clearSelectionAction();
        return;
      }
      const hostRect = host.getBoundingClientRect();
      const fromPaint = terminalSelectionPointFromHost(host);
      const point = clampTerminalSelectionPointToHost(
        fromPaint ?? fallbackPoint ?? { left: hostRect.left + 12, top: hostRect.top + 12 },
        host,
      );
      onSelectionActionChangeRef.current?.({ text, point });
    };

    let frame: number | null = null;
    const scheduleReport = (fallbackPoint?: TerminalSelectionPoint) => {
      if (frame !== null) cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => {
        frame = null;
        reportSelection(fallbackPoint);
      });
    };

    // Mirror transcript/workspace: only promote a floating action once the
    // gesture finishes. onSelectionChange clears an emptied selection so the
    // toolbar does not linger after click-away or clearSelection().
    const selectionChange = terminal.onSelectionChange(() => {
      if (!terminal.hasSelection() || normalizeTerminalSelectionText(terminal.getSelection()) === "") {
        clearSelectionAction();
      }
    });
    const onPointerUp = (event: PointerEvent) => {
      if (event.button !== 0) return;
      if ((event.target as HTMLElement | null)?.closest(".transcript-selection-action")) return;
      scheduleReport({ left: event.clientX, top: event.clientY + 8 });
    };
    const onKeyUp = () => {
      scheduleReport();
    };
    host.addEventListener("pointerup", onPointerUp);
    host.addEventListener("keyup", onKeyUp);

    const fitTerminal = () => {
      if (!fitEnabledRef.current) return;
      if (host.clientHeight < 32 || host.clientWidth < 32) return;
      fit.fit();
      const { cols, rows } = terminal;
      if (cols > 0 && rows > 0) void resize(tabId, session.id, cols, rows).catch(() => {});
    };
    const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(fitTerminal);
    observer?.observe(host);
    fitTerminal();
    return () => {
      if (frame !== null) cancelAnimationFrame(frame);
      observer?.disconnect();
      stopObservingTheme();
      input.dispose();
      outputResize.dispose();
      selectionChange.dispose();
      host.removeEventListener("pointerup", onPointerUp);
      host.removeEventListener("keyup", onKeyUp);
      unregister();
      clearSelectionAction();
      fitRef.current = null;
      terminalRef.current = null;
      terminal.dispose();
    };
  }, [resize, session.id, tabId, write]);

  useEffect(() => {
    if (!fitEnabled) return;
    const host = hostRef.current;
    const terminal = terminalRef.current;
    const fit = fitRef.current;
    if (!host || !terminal || !fit) return;
    if (host.clientHeight < 32 || host.clientWidth < 32) return;
    fit.fit();
    const { cols, rows } = terminal;
    if (cols > 0 && rows > 0) void resize(tabId, session.id, cols, rows).catch(() => {});
  }, [fitEnabled, resize, session.id, tabId]);

  return <div ref={hostRef} className="terminal-view" aria-label={session.title} />;
});
