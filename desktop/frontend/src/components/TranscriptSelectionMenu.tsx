import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
import { createPortal } from "react-dom";
import { MessageSquare } from "lucide-react";
import { ContextMenu, type ContextMenuPoint } from "./ContextMenu";
import { messageSelectionContextText, TRANSCRIPT_COPY_FAILED_EVENT } from "../lib/messageSelectionCopy";
import { writeClipboardText } from "../lib/clipboard";
import {
  detectShortcutPlatform,
  formatShortcutCombo,
  onShortcutsChanged,
  resolvedShortcutCombo,
  useGlobalShortcut,
} from "../lib/keyboardShortcuts";
import { useT } from "../lib/i18n";
import { transcriptSelectionStore } from "../lib/transcriptSelectionStore";
import { rowKeyForNode, transcriptSelectionPointClientRect } from "../lib/transcriptSelectionDom";
import { useToast } from "../lib/toast";

type SelectionAction =
  | { kind: "native"; text: string; point: ContextMenuPoint }
  | { kind: "logical"; snapshotId: number; sourceTabId: string; point: ContextMenuPoint };

const ACTION_EDGE_GAP = 8;

export function TranscriptSelectionMenu({
  enabled = true,
  resetKey,
  onAddToChat,
}: {
  enabled?: boolean;
  resetKey?: string | number;
  onAddToChat?: (text: string) => void;
}) {
  const t = useT();
  const { showToast } = useToast();
  const logicalSnapshot = useSyncExternalStore(
    transcriptSelectionStore.subscribe,
    transcriptSelectionStore.getSnapshot,
    transcriptSelectionStore.getSnapshot,
  );
  const [menu, setMenu] = useState<SelectionAction | null>(null);
  const [action, setAction] = useState<SelectionAction | null>(null);
  const [actionPoint, setActionPoint] = useState<ContextMenuPoint | null>(null);
  const actionRef = useRef<HTMLDivElement>(null);
  const dismissedRef = useRef<string | number | null>(null);
  const previousResetKeyRef = useRef(resetKey);
  const activeResetKeyRef = useRef(resetKey);
  activeResetKeyRef.current = resetKey;
  const shortcutPlatform = useMemo(() => detectShortcutPlatform(), []);
  const [shortcutRevision, setShortcutRevision] = useState(0);
  useEffect(() => onShortcutsChanged(() => setShortcutRevision((value) => value + 1)), []);
  const addShortcut = useMemo(
    () => formatShortcutCombo(resolvedShortcutCombo("selection.addToChat", shortcutPlatform), shortcutPlatform),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [shortcutPlatform, shortcutRevision],
  );

  const closeAction = useCallback(() => {
    setAction(null);
    setActionPoint(null);
  }, []);

  const reportCopyFailure = useCallback(() => {
    showToast(t("diag.copyFailed"), "error");
  }, [showToast, t]);

  useEffect(() => {
    const onFailure = () => reportCopyFailure();
    document.addEventListener(TRANSCRIPT_COPY_FAILED_EVENT, onFailure);
    return () => document.removeEventListener(TRANSCRIPT_COPY_FAILED_EVENT, onFailure);
  }, [reportCopyFailure]);

  useEffect(() => {
    if (previousResetKeyRef.current === resetKey) return;
    previousResetKeyRef.current = resetKey;
    dismissedRef.current = null;
    setMenu(null);
    closeAction();
    document.getSelection()?.removeAllRanges();
    transcriptSelectionStore.clear("tab-switch");
  }, [closeAction, resetKey]);

  const resolveLogical = useCallback(async (selection: Extract<SelectionAction, { kind: "logical" }>) => {
    const text = await transcriptSelectionStore.resolveText(selection.snapshotId);
    if (
      !text
      || !transcriptSelectionStore.isCurrent(selection.snapshotId, selection.sourceTabId)
      || String(activeResetKeyRef.current ?? "") !== selection.sourceTabId
    ) return null;
    return text;
  }, []);

  const copySelection = useCallback(async (selection: SelectionAction) => {
    if (selection.kind === "native") {
      const native = document.getSelection();
      const rangeSnapshot = native && !native.isCollapsed ? {
        anchorNode: native.anchorNode,
        anchorOffset: native.anchorOffset,
        focusNode: native.focusNode,
        focusOffset: native.focusOffset,
      } : null;
      const copied = await writeClipboardText(selection.text);
      const current = document.getSelection();
      if (!copied) {
        reportCopyFailure();
        return;
      }
      if (
        rangeSnapshot
        && current
        && !current.isCollapsed
        && current.anchorNode === rangeSnapshot.anchorNode
        && current.anchorOffset === rangeSnapshot.anchorOffset
        && current.focusNode === rangeSnapshot.focusNode
        && current.focusOffset === rangeSnapshot.focusOffset
      ) current.removeAllRanges();
      return;
    }
    const text = await resolveLogical(selection);
    if (!text) return;
    const copied = await writeClipboardText(text);
    if (!transcriptSelectionStore.isCurrent(selection.snapshotId, selection.sourceTabId)) return;
    if (!copied) {
      reportCopyFailure();
      return;
    }
    transcriptSelectionStore.clear("copy");
    closeAction();
  }, [closeAction, reportCopyFailure, resolveLogical]);

  const addSelectionToChat = useCallback(async () => {
    if (!action || !onAddToChat) return;
    if (action.kind === "native") {
      document.getSelection()?.removeAllRanges();
      closeAction();
      onAddToChat(action.text);
      return;
    }
    const text = await resolveLogical(action);
    if (!text) return;
    transcriptSelectionStore.clear("add-to-chat");
    closeAction();
    onAddToChat(text);
  }, [action, closeAction, onAddToChat, resolveLogical]);

  useGlobalShortcut(
    "selection.addToChat",
    () => { void addSelectionToChat(); },
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
    if (!enabled || !onAddToChat || logicalSnapshot.mode !== "logical-settled") {
      if (action?.kind === "logical") closeAction();
      return;
    }
    if (logicalSnapshot.tabId !== String(resetKey ?? "") || !logicalSnapshot.focus) return;
    if (dismissedRef.current === logicalSnapshot.id) return;
    const rect = transcriptSelectionPointClientRect(logicalSnapshot.focus);
    setAction({
      kind: "logical",
      snapshotId: logicalSnapshot.id,
      sourceTabId: logicalSnapshot.tabId,
      point: rect ? { left: rect.right, top: rect.bottom + 8 } : { left: 12, top: 12 },
    });
  }, [action?.kind, closeAction, enabled, logicalSnapshot, onAddToChat, resetKey]);

  useEffect(() => {
    const onContextMenu = (event: MouseEvent) => {
      if (!enabled || typeof window === "undefined" || !window.runtime) return;
      const snapshot = transcriptSelectionStore.getSnapshot();
      const rowKey = rowKeyForNode(event.target instanceof Node ? event.target : null);
      if (
        rowKey
        && (snapshot.mode === "logical-dragging" || snapshot.mode === "logical-settled")
        && transcriptSelectionStore.isRowSelected(snapshot.id, rowKey)
      ) {
        event.preventDefault();
        setMenu({
          kind: "logical",
          snapshotId: snapshot.id,
          sourceTabId: snapshot.tabId,
          point: menuPointFromEvent(event),
        });
        return;
      }
      const selected = messageSelectionContextText(document, event.target);
      if (selected == null) return;
      event.preventDefault();
      setMenu({ kind: "native", text: selected, point: menuPointFromEvent(event) });
    };
    document.addEventListener("contextmenu", onContextMenu);
    return () => document.removeEventListener("contextmenu", onContextMenu);
  }, [enabled]);

  useEffect(() => {
    if (enabled && onAddToChat) return;
    setMenu(null);
    closeAction();
    document.getSelection()?.removeAllRanges();
    transcriptSelectionStore.clear("selection-actions-disabled");
  }, [closeAction, enabled, onAddToChat]);

  useEffect(() => {
    if (!enabled || !onAddToChat) return;
    let frame: number | null = null;
    const showForTarget = (target: EventTarget | null) => {
      if (transcriptSelectionStore.isLogical()) return;
      const selected = messageSelectionContextText(document, target);
      const selection = document.getSelection();
      const range = selection?.rangeCount ? selection.getRangeAt(selection.rangeCount - 1) : null;
      if (selected == null || !range) {
        dismissedRef.current = null;
        if (action?.kind === "native") closeAction();
        return;
      }
      if (dismissedRef.current === selected) return;
      dismissedRef.current = null;
      const rect = typeof range.getBoundingClientRect === "function" ? range.getBoundingClientRect() : null;
      setAction({
        kind: "native",
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
    const onPointerDown = (event: PointerEvent) => {
      if (event.button !== 0) return;
      const target = event.target instanceof Element ? event.target : null;
      if (target?.closest(".transcript-selection-action")) return;
      const snapshot = transcriptSelectionStore.getSnapshot();
      if (snapshot.mode === "logical-dragging" || snapshot.mode === "logical-settled") {
        transcriptSelectionStore.clear("new-pointer");
      }
      const selection = document.getSelection();
      if (selection && !selection.isCollapsed) selection.removeAllRanges();
      dismissedRef.current = null;
      closeAction();
    };
    const onKeyUp = (event: KeyboardEvent) => {
      const selection = document.getSelection();
      const target = selection?.focusNode instanceof Element
        ? selection.focusNode
        : selection?.focusNode?.parentElement ?? event.target;
      scheduleShow(target);
    };
    const onSelectionChange = () => {
      if (transcriptSelectionStore.isLogical()) return;
      const selection = document.getSelection();
      if (!selection || selection.isCollapsed || selection.toString().trim() === "") {
        dismissedRef.current = null;
        if (action?.kind === "native") closeAction();
      }
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "Escape" || !action) return;
      dismissedRef.current = action.kind === "native" ? action.text : action.snapshotId;
      if (action.kind === "logical") transcriptSelectionStore.clear("escape");
      closeAction();
    };
    const closeNative = () => {
      if (action?.kind === "native") closeAction();
    };
    document.addEventListener("pointerdown", onPointerDown);
    document.addEventListener("pointerup", onPointerUp);
    document.addEventListener("keyup", onKeyUp);
    document.addEventListener("keydown", onKeyDown);
    document.addEventListener("selectionchange", onSelectionChange);
    window.addEventListener("resize", closeNative);
    window.addEventListener("scroll", closeNative, true);
    return () => {
      if (frame !== null) cancelAnimationFrame(frame);
      document.removeEventListener("pointerdown", onPointerDown);
      document.removeEventListener("pointerup", onPointerUp);
      document.removeEventListener("keyup", onKeyUp);
      document.removeEventListener("keydown", onKeyDown);
      document.removeEventListener("selectionchange", onSelectionChange);
      window.removeEventListener("resize", closeNative);
      window.removeEventListener("scroll", closeNative, true);
    };
  }, [action, closeAction, enabled, onAddToChat]);

  return <>
    <ContextMenu
      open={menu != null}
      point={menu?.point ?? null}
      minWidth={140}
      ariaLabel={t("common.copy")}
      items={[{
        key: "copy",
        label: t("common.copy"),
        shortcut: formatShortcutCombo(
          shortcutPlatform === "darwin" ? { key: "c", meta: true } : { key: "c", ctrl: true },
          shortcutPlatform,
        ),
        onSelect: () => {
          if (menu) void copySelection(menu);
          setMenu(null);
        },
      }]}
      onClose={() => setMenu(null)}
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
        <button type="button" onClick={() => void addSelectionToChat()}>
          <MessageSquare size={14} aria-hidden="true" />
          <span>{t("selection.addToChat")}</span>
          <kbd>{addShortcut}</kbd>
        </button>
      </div>,
      document.body,
    )}
  </>;
}

function menuPointFromEvent(event: MouseEvent): ContextMenuPoint {
  if (event.clientX > 0 || event.clientY > 0) return { left: event.clientX, top: event.clientY };
  const range = document.getSelection()?.rangeCount ? document.getSelection()?.getRangeAt(0) : null;
  const rect = typeof range?.getBoundingClientRect === "function" ? range.getBoundingClientRect() : null;
  if (rect && (rect.width > 0 || rect.height > 0)) return { left: rect.left, top: rect.bottom + 4 };
  return { left: 12, top: 12 };
}
