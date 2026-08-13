import { forwardRef, useEffect, useImperativeHandle, useMemo, useRef, useState } from "react";
import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import { Clipboard, Copy, MessageSquare } from "lucide-react";
import "@xterm/xterm/css/xterm.css";

import { useTerminalStore } from "../store/terminal";
import { registerTerminalSink, startTerminalEventBridge } from "../lib/terminalEvents";
import { writeClipboardText } from "../lib/clipboard";
import { detectShortcutPlatform, formatShortcutCombo } from "../lib/keyboardShortcuts";
import { useT } from "../lib/i18n";
import {
  clampTerminalSelectionPointToHost,
  handleTerminalCopyKey,
  normalizeTerminalSelectionText,
  readTerminalClipboardText,
  terminalSelectionPointFromHost,
  type TerminalSelectionPoint,
} from "../lib/terminalSelection";
import { observeTerminalTheme, terminalThemeForElement } from "../lib/terminalTheme";
import { useToast } from "../lib/toast";
import type { TerminalSessionView } from "../lib/types";
import { ContextMenu, type ContextMenuPoint } from "./ContextMenu";

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
  onSelectionActionChange?: (action: TerminalSelectionAction | null) => void;
  onAddToChat?: (text: string) => void;
}>(function TerminalView({ tabId, session, onSelectionActionChange, onAddToChat }, ref) {
  const hostRef = useRef<HTMLDivElement>(null);
  const terminalRef = useRef<Terminal | null>(null);
  const onSelectionActionChangeRef = useRef(onSelectionActionChange);
  const onAddToChatRef = useRef(onAddToChat);
  const [menu, setMenu] = useState<ContextMenuPoint | null>(null);
  const [selectionText, setSelectionText] = useState("");
  const selectionTextRef = useRef("");
  const write = useTerminalStore((state) => state.write);
  const resize = useTerminalStore((state) => state.resize);
  const { showToast } = useToast();
  const t = useT();
  onSelectionActionChangeRef.current = onSelectionActionChange;
  onAddToChatRef.current = onAddToChat;

  useImperativeHandle(ref, () => ({
    clearSelection: () => {
      terminalRef.current?.clearSelection();
      setSelectionText("");
      onSelectionActionChangeRef.current?.(null);
    },
  }), []);

  const updateSelection = () => {
    const text = normalizeTerminalSelectionText(terminalRef.current?.getSelection() ?? "");
    selectionTextRef.current = text;
    setSelectionText(text);
  };

  const clearSelectionAction = () => {
    onSelectionActionChangeRef.current?.(null);
  };

  const reportSelection = (fallbackPoint?: TerminalSelectionPoint) => {
    const text = normalizeTerminalSelectionText(terminalRef.current?.getSelection() ?? "");
    if (!text) {
      clearSelectionAction();
      return;
    }
    const host = hostRef.current;
    if (!host) return;
    const hostRect = host.getBoundingClientRect();
    const fromPaint = terminalSelectionPointFromHost(host);
    const point = clampTerminalSelectionPointToHost(
      fromPaint ?? fallbackPoint ?? { left: hostRect.left + 12, top: hostRect.top + 12 },
      host,
    );
    onSelectionActionChangeRef.current?.({ text, point });
  };

  const copySelection = async () => {
    const text = selectionTextRef.current;
    if (!text) return;
    const copied = await writeClipboardText(text);
    if (!copied) {
      showToast(t("diag.copyFailed"), "error");
      return;
    }
    terminalRef.current?.clearSelection();
    setSelectionText("");
    clearSelectionAction();
  };

  const pasteFromClipboard = async () => {
    const text = await readTerminalClipboardText();
    if (!text) return;
    terminalRef.current?.paste(text);
  };

  const addSelectionToChat = () => {
    const text = selectionTextRef.current;
    if (!text) return;
    onAddToChatRef.current?.(text);
    terminalRef.current?.clearSelection();
    setSelectionText("");
    clearSelectionAction();
  };

  const shortcutPlatform = useMemo(() => detectShortcutPlatform(), []);

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
    terminal.loadAddon(fit);
    terminal.open(host);
    const updateTheme = () => {
      terminal.options.theme = terminalThemeForElement(host);
    };
    const stopObservingTheme = observeTerminalTheme(host, updateTheme);
    const unregister = registerTerminalSink(session.id, (bytes) => terminal.write(bytes));
    const input = terminal.onData((data) => { void write(tabId, session.id, data).catch(() => {}); });
    const outputResize = terminal.onResize(({ cols, rows }) => { void resize(tabId, session.id, cols, rows).catch(() => {}); });
    terminal.attachCustomKeyEventHandler((event) => {
      if (event.type !== "keydown") return true;
      const decision = handleTerminalCopyKey({
        key: event.key,
        ctrlKey: event.ctrlKey,
        metaKey: event.metaKey,
        altKey: event.altKey,
        hasSelection: () => terminal.hasSelection(),
        getSelection: () => terminal.getSelection(),
      });
      if (decision.intercepted) {
        void writeClipboardText(decision.text).then((copied) => {
          if (!copied) {
            showToast(t("diag.copyFailed"), "error");
            return;
          }
          terminal.clearSelection();
          setSelectionText("");
          clearSelectionAction();
        });
        return false;
      }
      return true;
    });
    const selectionChange = terminal.onSelectionChange(() => {
      updateSelection();
      if (!terminal.hasSelection() || normalizeTerminalSelectionText(terminal.getSelection()) === "") {
        clearSelectionAction();
      }
    });
    let frame: number | null = null;
    const onPointerUp = (event: PointerEvent) => {
      if (event.button !== 0) return;
      if (frame !== null) cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => {
        frame = null;
        reportSelection({ left: event.clientX, top: event.clientY + 8 });
      });
    };
    const onContextMenu = (event: MouseEvent) => {
      event.preventDefault();
      updateSelection();
      setMenu({ left: event.clientX, top: event.clientY });
    };
    host.addEventListener("pointerup", onPointerUp);
    host.addEventListener("contextmenu", onContextMenu);
    const fitTerminal = () => {
      fit.fit();
      const { cols, rows } = terminal;
      if (cols > 0 && rows > 0) void resize(tabId, session.id, cols, rows).catch(() => {});
    };
    const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(fitTerminal);
    observer?.observe(host);
    fitTerminal();
    return () => {
      if (frame !== null) cancelAnimationFrame(frame);
      host.removeEventListener("pointerup", onPointerUp);
      host.removeEventListener("contextmenu", onContextMenu);
      observer?.disconnect();
      stopObservingTheme();
      selectionChange.dispose();
      input.dispose();
      outputResize.dispose();
      unregister();
      terminalRef.current = null;
      terminal.dispose();
      // A session switch disposes this terminal while TerminalPanel stays
      // mounted; drop any floating selection action so its stale text can
      // never be added to the chat of the newly active session.
      onSelectionActionChangeRef.current?.(null);
    };
  }, [resize, session.id, tabId, write]);

  const copyShortcut = formatShortcutCombo(
    shortcutPlatform === "darwin" ? { key: "c", meta: true } : { key: "c", ctrl: true },
    shortcutPlatform,
  );
  const pasteShortcut = formatShortcutCombo(
    shortcutPlatform === "darwin" ? { key: "v", meta: true } : { key: "v", ctrl: true },
    shortcutPlatform,
  );

  return <>
    <ContextMenu
      open={menu != null}
      point={menu}
      minWidth={180}
      ariaLabel={t("terminal.title")}
      items={[
        {
          key: "copy",
          label: t("common.copy"),
          icon: <Copy size={14} />,
          shortcut: copyShortcut,
          disabled: !selectionText,
          onSelect: () => {
            setMenu(null);
            void copySelection();
          },
        },
        {
          key: "paste",
          label: t("common.paste"),
          icon: <Clipboard size={14} />,
          shortcut: pasteShortcut,
          onSelect: () => {
            setMenu(null);
            void pasteFromClipboard();
          },
        },
        { type: "separator", key: "terminal-menu-separator" },
        {
          key: "add-to-chat",
          label: t("selection.addToChat"),
          icon: <MessageSquare size={14} />,
          disabled: !selectionText,
          onSelect: () => {
            setMenu(null);
            addSelectionToChat();
          },
        },
      ]}
      onClose={() => setMenu(null)}
    />
    <div ref={hostRef} className="terminal-view" aria-label={session.title} />
  </>;
});
