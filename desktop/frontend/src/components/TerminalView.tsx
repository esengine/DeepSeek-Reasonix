import { forwardRef, useEffect, useImperativeHandle, useLayoutEffect, useMemo, useRef, useState } from "react";
import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import { Clipboard, Copy, MessageSquare } from "lucide-react";
import "@xterm/xterm/css/xterm.css";

import { useTerminalStore } from "../store/terminal";
import { startTerminalEventBridge } from "../lib/terminalEvents";
import { registerTerminalSink, type TerminalSinkSubscription } from "../lib/terminalSink";
import { writeClipboardText } from "../lib/clipboard";
import { detectShortcutPlatform, formatShortcutCombo } from "../lib/keyboardShortcuts";
import { useT } from "../lib/i18n";
import {
  clampTerminalSelectionPointToHost,
  createTerminalSelectionLifecycle,
  handleTerminalCopyKey,
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
  open?: boolean;
  fitEnabled?: boolean;
  onSelectionActionChange?: (action: TerminalSelectionAction | null) => void;
  onAddToChat?: (text: string) => void;
}>(function TerminalView({ tabId, session, open = true, fitEnabled = true, onSelectionActionChange, onAddToChat }, ref) {
  const hostRef = useRef<HTMLDivElement>(null);
  const terminalRef = useRef<Terminal | null>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const terminalSinkRef = useRef<TerminalSinkSubscription | null>(null);
  const fitEnabledRef = useRef(fitEnabled);
  const openRef = useRef(open);
  const selectionLifecycleRef = useRef(createTerminalSelectionLifecycle<Terminal>());
  const onSelectionActionChangeRef = useRef(onSelectionActionChange);
  const onAddToChatRef = useRef(onAddToChat);
  const [menu, setMenu] = useState<ContextMenuPoint | null>(null);
  const [selectionText, setSelectionText] = useState("");
  const selectionTextRef = useRef("");
  const write = useTerminalStore((state) => state.write);
  const resize = useTerminalStore((state) => state.resize);
  const { showToast } = useToast();
  const t = useT();
  fitEnabledRef.current = fitEnabled;
  openRef.current = open;
  onSelectionActionChangeRef.current = onSelectionActionChange;
  onAddToChatRef.current = onAddToChat;

  const clearSelectionState = (terminal = terminalRef.current) => {
    selectionLifecycleRef.current.noteSelectionChange();
    terminal?.clearSelection();
    selectionTextRef.current = "";
    setSelectionText("");
    onSelectionActionChangeRef.current?.(null);
  };

  useImperativeHandle(ref, () => ({
    clearSelection: clearSelectionState,
  }), []);

  const updateSelection = () => {
    selectionLifecycleRef.current.noteSelectionChange();
    const text = terminalRef.current?.getSelection() ?? "";
    selectionTextRef.current = text;
    setSelectionText(text);
  };

  const clearSelectionAction = () => {
    onSelectionActionChangeRef.current?.(null);
  };

  const reportSelection = (fallbackPoint?: TerminalSelectionPoint) => {
    if (!openRef.current) {
      clearSelectionAction();
      return;
    }
    const terminal = terminalRef.current;
    if (!terminal?.hasSelection()) {
      clearSelectionAction();
      return;
    }
    const text = terminal.getSelection();
    const host = hostRef.current;
    // A queued pointer-up frame can outlive a rapid tab/session switch. The
    // detached host must not recreate its selection action after the owner
    // panel synchronously cleared it.
    if (!host?.isConnected) {
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

  const copySelection = async () => {
    const operation = selectionLifecycleRef.current.captureSelection();
    const text = selectionTextRef.current;
    if (!operation || !text) return;
    const copied = await writeClipboardText(text);
    if (!selectionLifecycleRef.current.isCurrentSelection(operation)) return;
    if (!copied) {
      showToast(t("diag.copyFailed"), "error");
      return;
    }
    clearSelectionState(operation.terminal);
  };

  const pasteFromClipboard = async () => {
    const operation = selectionLifecycleRef.current.capture();
    if (!operation) return;
    const text = await readTerminalClipboardText();
    if (!text || !selectionLifecycleRef.current.isCurrent(operation)) return;
    operation.terminal.paste(text);
  };

  const addSelectionToChat = () => {
    const text = selectionTextRef.current;
    if (!text) return;
    onAddToChatRef.current?.(text);
    clearSelectionState();
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
    selectionLifecycleRef.current.activate(terminal);
    selectionTextRef.current = "";
    setSelectionText("");
    const fit = new FitAddon();
    fitRef.current = fit;
    terminal.loadAddon(fit);
    terminal.open(host);
    const updateTheme = () => {
      terminal.options.theme = terminalThemeForElement(host);
    };
    const stopObservingTheme = observeTerminalTheme(host, updateTheme);
    const terminalSink = registerTerminalSink(session.id, (bytes) => terminal.write(bytes), openRef.current);
    terminalSinkRef.current = terminalSink;
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
        const selectionOperation = selectionLifecycleRef.current.captureSelection();
        if (!selectionOperation) return false;
        void writeClipboardText(decision.text).then((copied) => {
          if (!selectionLifecycleRef.current.isCurrentSelection(selectionOperation)) return;
          if (!copied) {
            showToast(t("diag.copyFailed"), "error");
            return;
          }
          clearSelectionState(terminal);
        });
        return false;
      }
      return true;
    });
    const selectionChange = terminal.onSelectionChange(() => {
      updateSelection();
      if (!terminal.hasSelection()) {
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
      host.removeEventListener("pointerup", onPointerUp);
      host.removeEventListener("contextmenu", onContextMenu);
      observer?.disconnect();
      stopObservingTheme();
      selectionChange.dispose();
      input.dispose();
      outputResize.dispose();
      terminalSink.dispose();
      if (terminalSinkRef.current === terminalSink) terminalSinkRef.current = null;
      selectionLifecycleRef.current.deactivate(terminal);
      if (fitRef.current === fit) fitRef.current = null;
      if (terminalRef.current === terminal) terminalRef.current = null;
      terminal.dispose();
      // A session switch disposes this terminal while TerminalPanel stays
      // mounted; drop any floating selection action so its stale text can
      // never be added to the chat of the newly active session.
      onSelectionActionChangeRef.current?.(null);
    };
  }, [resize, session.id, tabId, write]);

  useLayoutEffect(() => {
    terminalSinkRef.current?.setActive(open);
  }, [open]);

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

  useEffect(() => {
    if (!open) setMenu(null);
  }, [open]);

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
      open={open && menu != null}
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
