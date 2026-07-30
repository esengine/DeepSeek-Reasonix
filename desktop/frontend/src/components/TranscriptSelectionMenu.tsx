import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { MessageSquare } from "lucide-react";
import { ContextMenu, type ContextMenuPoint } from "./ContextMenu";
import { messageSelectionContextText } from "../lib/messageSelectionCopy";
import { writeClipboardText } from "../lib/clipboard";
import {
  detectShortcutPlatform,
  formatShortcutCombo,
  onShortcutsChanged,
  resolvedShortcutCombo,
  useGlobalShortcut,
} from "../lib/keyboardShortcuts";
import { useT } from "../lib/i18n";

// Inside the Wails shell main.tsx suppresses the webview's default context
// menu (its Reload/Back/Inspect entries can navigate away from the app), which
// also removes the native Copy menu for selected transcript text — ⌘C still
// works, but the right-click path is dead. This mounts one document-level
// listener that offers an app-drawn Copy menu whenever a suppressed selection
// menu would have applied, gated exactly like the ⌘C interceptor. It stays
// inert in a plain browser (no window.runtime), where the native menu opens.
type SelectionAction = {
  text: string;
  point: ContextMenuPoint;
};

const ACTION_EDGE_GAP = 8;

export function TranscriptSelectionMenu({
  enabled = true,
  resetKey,
  onAddToChat,
}: {
  enabled?: boolean;
  // Identifies the transcript the selection was made in (the active tab).
  // The overlay captures only text, while onAddToChat routes to whatever is
  // active at click time — so any surviving overlay must be discarded when
  // the source changes or a selection from session A could land in session B.
  resetKey?: string | number;
  onAddToChat?: (text: string) => void;
}) {
  const t = useT();
  const [point, setPoint] = useState<ContextMenuPoint | null>(null);
  const [text, setText] = useState("");
  const [action, setAction] = useState<SelectionAction | null>(null);
  const [actionPoint, setActionPoint] = useState<ContextMenuPoint | null>(null);
  const actionRef = useRef<HTMLDivElement>(null);
  // Escape dismisses the floating action but browsers keep the text selection,
  // so the trailing keyup would immediately re-show it. Remember the dismissed
  // selection and stay hidden until it changes or a new pointer gesture lands.
  const dismissedRef = useRef<string | null>(null);
  const shortcutPlatform = useMemo(() => detectShortcutPlatform(), []);
  const [shortcutRevision, setShortcutRevision] = useState(0);
  useEffect(() => onShortcutsChanged(() => setShortcutRevision((value) => value + 1)), []);
  const addShortcut = useMemo(
    () => formatShortcutCombo(resolvedShortcutCombo("selection.addToChat", shortcutPlatform), shortcutPlatform),
    // shortcutRevision re-resolves the combo after the user rebinds it in settings.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [shortcutPlatform, shortcutRevision],
  );

  const closeAction = useCallback(() => {
    setAction(null);
    setActionPoint(null);
  }, []);

  // Drop every piece of captured selection state when the source transcript
  // changes: the floating action, the copy menu, and the Escape-dismissal
  // memory all describe the previous tab's content.
  const lastResetKeyRef = useRef(resetKey);
  useEffect(() => {
    if (lastResetKeyRef.current === resetKey) return;
    lastResetKeyRef.current = resetKey;
    dismissedRef.current = null;
    setPoint(null);
    setText("");
    closeAction();
  }, [closeAction, resetKey]);

  const addSelectionToChat = useCallback(() => {
    if (!action || !onAddToChat) return;
    const selectedText = action.text;
    document.getSelection()?.removeAllRanges();
    closeAction();
    onAddToChat(selectedText);
  }, [action, closeAction, onAddToChat]);

  // The shortcut is registered in SHORTCUT_DEFINITIONS so settings can rebind
  // it and conflict-check it against other actions; it only arms while the
  // floating action is visible.
  useGlobalShortcut(
    "selection.addToChat",
    addSelectionToChat,
    [],
    Boolean(action) && enabled && Boolean(onAddToChat),
  );

  useLayoutEffect(() => {
    if (!action) {
      setActionPoint(null);
      return;
    }
    const rect = actionRef.current?.getBoundingClientRect();
    if (!rect) {
      setActionPoint(action.point);
      return;
    }
    setActionPoint({
      left: Math.min(
        Math.max(ACTION_EDGE_GAP, action.point.left),
        Math.max(ACTION_EDGE_GAP, window.innerWidth - rect.width - ACTION_EDGE_GAP),
      ),
      top: Math.min(
        Math.max(ACTION_EDGE_GAP, action.point.top),
        Math.max(ACTION_EDGE_GAP, window.innerHeight - rect.height - ACTION_EDGE_GAP),
      ),
    });
  }, [action]);

  useEffect(() => {
    const onContextMenu = (event: MouseEvent) => {
      if (typeof window === "undefined" || !window.runtime) return;
      const selected = messageSelectionContextText(document, event.target);
      if (selected == null) return;
      event.preventDefault();
      setText(selected);
      setPoint(menuPointFromEvent(event));
    };
    document.addEventListener("contextmenu", onContextMenu);
    return () => document.removeEventListener("contextmenu", onContextMenu);
  }, []);

  useEffect(() => {
    if (!enabled || !onAddToChat) {
      closeAction();
      return;
    }

    let frame: number | null = null;
    const showForTarget = (target: EventTarget | null) => {
      const selected = messageSelectionContextText(document, target);
      const selection = document.getSelection();
      const range = selection?.rangeCount ? selection.getRangeAt(selection.rangeCount - 1) : null;
      if (selected == null || !range) {
        dismissedRef.current = null;
        closeAction();
        return;
      }
      if (dismissedRef.current === selected) return;
      dismissedRef.current = null;
      const rect = typeof range.getBoundingClientRect === "function" ? range.getBoundingClientRect() : null;
      setAction({
        text: selected,
        point: rect && (rect.width > 0 || rect.height > 0)
          ? { left: rect.right, top: rect.bottom + 8 }
          : { left: 12, top: 12 },
      });
    };
    const scheduleShow = (target: EventTarget | null) => {
      if (frame !== null) cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => {
        frame = null;
        showForTarget(target);
      });
    };
    const onPointerUp = (event: PointerEvent) => {
      if (event.button !== 0) return;
      dismissedRef.current = null;
      scheduleShow(event.target);
    };
    const onKeyUp = (event: KeyboardEvent) => {
      const selection = document.getSelection();
      const target = selection?.focusNode instanceof Element
        ? selection.focusNode
        : selection?.focusNode?.parentElement ?? event.target;
      scheduleShow(target);
    };
    const onSelectionChange = () => {
      const selection = document.getSelection();
      if (!selection || selection.isCollapsed || selection.toString().trim() === "") {
        dismissedRef.current = null;
        closeAction();
      }
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "Escape" || !action) return;
      dismissedRef.current = action.text;
      closeAction();
    };
    const close = () => closeAction();

    document.addEventListener("pointerup", onPointerUp);
    document.addEventListener("keyup", onKeyUp);
    document.addEventListener("keydown", onKeyDown);
    document.addEventListener("selectionchange", onSelectionChange);
    window.addEventListener("resize", close);
    window.addEventListener("scroll", close, true);
    return () => {
      if (frame !== null) cancelAnimationFrame(frame);
      document.removeEventListener("pointerup", onPointerUp);
      document.removeEventListener("keyup", onKeyUp);
      document.removeEventListener("keydown", onKeyDown);
      document.removeEventListener("selectionchange", onSelectionChange);
      window.removeEventListener("resize", close);
      window.removeEventListener("scroll", close, true);
    };
  }, [action, closeAction, enabled, onAddToChat]);

  return <>
    <ContextMenu
      open={point != null}
      point={point}
      minWidth={140}
      ariaLabel={t("common.copy")}
      items={[
        {
          key: "copy",
          label: t("common.copy"),
          shortcut: formatShortcutCombo(
            shortcutPlatform === "darwin" ? { key: "c", meta: true } : { key: "c", ctrl: true },
            shortcutPlatform,
          ),
          onSelect: () => {
            void writeClipboardText(text);
            setPoint(null);
          },
        },
      ]}
      onClose={() => setPoint(null)}
    />
    {action && typeof document !== "undefined" && createPortal(
      <div
        ref={actionRef}
        className="transcript-selection-action"
        role="toolbar"
        aria-label={t("selection.actions")}
        style={{
          left: actionPoint?.left ?? action.point.left,
          top: actionPoint?.top ?? action.point.top,
          visibility: actionPoint ? "visible" : "hidden",
        }}
        onMouseDown={(event) => event.preventDefault()}
      >
        <button type="button" onClick={addSelectionToChat}>
          <MessageSquare size={14} aria-hidden="true" />
          <span>{t("selection.addToChat")}</span>
          <kbd>{addShortcut}</kbd>
        </button>
      </div>,
      document.body,
    )}
  </>;
}

// The keyboard context-menu key fires contextmenu at (0, 0); anchor the menu
// to the selection instead so it opens next to the highlighted text.
function menuPointFromEvent(event: MouseEvent): ContextMenuPoint {
  if (event.clientX > 0 || event.clientY > 0) {
    return { left: event.clientX, top: event.clientY };
  }
  const range = document.getSelection()?.rangeCount ? document.getSelection()?.getRangeAt(0) : null;
  const rect = typeof range?.getBoundingClientRect === "function" ? range.getBoundingClientRect() : null;
  if (rect && (rect.width > 0 || rect.height > 0)) {
    return { left: rect.left, top: rect.bottom + 4 };
  }
  return { left: 12, top: 12 };
}
