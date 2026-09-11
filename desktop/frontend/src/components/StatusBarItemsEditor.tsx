import { useEffect, useId, useRef, useState, type MouseEvent as ReactMouseEvent, type PointerEvent } from "react";
import { Check, ChevronDown, ChevronUp, EyeOff, GripVertical } from "lucide-react";
import { useT } from "../lib/i18n";
import { DEFAULT_STATUS_BAR_ITEMS, type StatusBarItemId } from "../lib/statusBarItems";
import { Tooltip } from "./Tooltip";

type DropPlacement = "before" | "after";
type DragTarget = { id: StatusBarItemId; placement: DropPlacement };

export function StatusBarItemsEditor({
  items,
  busy,
  onChange,
  itemLabel,
}: {
  items: StatusBarItemId[];
  busy: boolean;
  onChange: (items: StatusBarItemId[]) => void;
  itemLabel: (id: StatusBarItemId) => string;
}) {
  const t = useT();
  const [expanded, setExpanded] = useState(false);
  const [draggingItem, setDraggingItem] = useState<StatusBarItemId | null>(null);
  const [dragTarget, setDragTargetState] = useState<DragTarget | null>(null);
  const [dropZone, setDropZoneState] = useState<"hidden" | null>(null);
  const draggingItemRef = useRef<StatusBarItemId | null>(null);
  const dragTargetRef = useRef<DragTarget | null>(null);
  const dropZoneRef = useRef<"hidden" | null>(null);
  const mouseDragCleanupRef = useRef<(() => void) | null>(null);
  const panelId = useId();
  const visibleItems = items;
  const visibleSet = new Set<StatusBarItemId>(visibleItems);
  const hiddenItems = DEFAULT_STATUS_BAR_ITEMS.filter((id) => !visibleSet.has(id));
  const visiblePaneLabel = t("settings.statusBarItemsVisible", { count: visibleItems.length });
  const hiddenPaneLabel = t("settings.statusBarItemsHidden", { count: hiddenItems.length });
  const isDefault = visibleItems.length === DEFAULT_STATUS_BAR_ITEMS.length &&
    visibleItems.every((id, index) => id === DEFAULT_STATUS_BAR_ITEMS[index]);

  useEffect(() => () => mouseDragCleanupRef.current?.(), []);

  const setDragTarget = (target: DragTarget | null) => {
    const current = dragTargetRef.current;
    if (current?.id === target?.id && current?.placement === target?.placement) return;
    dragTargetRef.current = target;
    setDragTargetState(target);
  };
  const setDropZone = (zone: "hidden" | null) => {
    if (dropZoneRef.current === zone) return;
    dropZoneRef.current = zone;
    setDropZoneState(zone);
  };
  const itemFromPoint = (x: number, y: number): DragTarget | null => {
    const row = document.elementFromPoint(x, y)?.closest<HTMLElement>("[data-statusbar-setting-item]");
    const id = row?.dataset.statusbarSettingItem as StatusBarItemId | undefined;
    if (!row || !id || !visibleItems.includes(id)) return null;
    const rect = row.getBoundingClientRect();
    return { id, placement: y < rect.top + rect.height / 2 ? "before" : "after" };
  };
  const hiddenZoneFromPoint = (x: number, y: number): boolean =>
    document.elementFromPoint(x, y)?.closest<HTMLElement>("[data-statusbar-drop-zone='hidden']") != null;
  const reorderItem = (fromId: StatusBarItemId, toId: StatusBarItemId, placement: DropPlacement) => {
    const fromIndex = visibleItems.indexOf(fromId);
    const toIndex = visibleItems.indexOf(toId);
    if (fromIndex < 0 || toIndex < 0 || fromIndex === toIndex) return;
    const next = visibleItems.filter((item) => item !== fromId);
    const insertAt = next.indexOf(toId);
    if (insertAt < 0) return;
    next.splice(placement === "after" ? insertAt + 1 : insertAt, 0, fromId);
    if (!next.every((item, index) => item === visibleItems[index])) onChange(next);
  };
  const toggleItem = (id: StatusBarItemId) => {
    if (visibleSet.has(id)) {
      if (visibleItems.length > 1) onChange(visibleItems.filter((item) => item !== id));
      return;
    }
    onChange([...visibleItems, id]);
  };
  const moveItem = (id: StatusBarItemId, direction: -1 | 1) => {
    const index = visibleItems.indexOf(id);
    const nextIndex = index + direction;
    if (index < 0 || nextIndex < 0 || nextIndex >= visibleItems.length) return;
    const next = [...visibleItems];
    [next[index], next[nextIndex]] = [next[nextIndex], next[index]];
    onChange(next);
  };
  const beginDrag = (id: StatusBarItemId): boolean => {
    if (busy || !visibleSet.has(id)) return false;
    mouseDragCleanupRef.current?.();
    mouseDragCleanupRef.current = null;
    draggingItemRef.current = id;
    dragTargetRef.current = null;
    dropZoneRef.current = null;
    setDraggingItem(id);
    setDragTargetState(null);
    setDropZoneState(null);
    return true;
  };
  const updateDrag = (clientX: number, clientY: number) => {
    const draggingId = draggingItemRef.current;
    if (!draggingId) return;
    if (hiddenZoneFromPoint(clientX, clientY)) {
      setDragTarget(null);
      setDropZone("hidden");
      return;
    }
    setDropZone(null);
    const target = itemFromPoint(clientX, clientY);
    setDragTarget(target && target.id !== draggingId ? target : null);
  };
  const finishDrag = (clientX?: number, clientY?: number) => {
    const draggingId = draggingItemRef.current;
    let target = dragTargetRef.current;
    let zone = dropZoneRef.current;
    if (draggingId && clientX !== undefined && clientY !== undefined) {
      zone = hiddenZoneFromPoint(clientX, clientY) ? "hidden" : null;
      const pointerTarget = zone ? null : itemFromPoint(clientX, clientY);
      if (pointerTarget && pointerTarget.id !== draggingId) target = pointerTarget;
    }
    if (draggingId && zone === "hidden" && visibleItems.length > 1) {
      onChange(visibleItems.filter((item) => item !== draggingId));
    } else if (draggingId && target) {
      reorderItem(draggingId, target.id, target.placement);
    }
    draggingItemRef.current = null;
    dragTargetRef.current = null;
    dropZoneRef.current = null;
    setDraggingItem(null);
    setDragTargetState(null);
    setDropZoneState(null);
  };
  const cancelDrag = () => {
    mouseDragCleanupRef.current?.();
    mouseDragCleanupRef.current = null;
    draggingItemRef.current = null;
    dragTargetRef.current = null;
    dropZoneRef.current = null;
    setDraggingItem(null);
    setDragTargetState(null);
    setDropZoneState(null);
  };
  const startPointerDrag = (event: PointerEvent<HTMLElement>, id: StatusBarItemId) => {
    if (event.button !== 0 || !beginDrag(id)) return;
    event.preventDefault();
    event.currentTarget.setPointerCapture(event.pointerId);
  };
  const movePointerDrag = (event: PointerEvent<HTMLElement>) => {
    if (!draggingItemRef.current) return;
    event.preventDefault();
    updateDrag(event.clientX, event.clientY);
  };
  const endPointerDrag = (event: PointerEvent<HTMLElement>) => {
    if (!draggingItemRef.current) return;
    event.preventDefault();
    try { event.currentTarget.releasePointerCapture(event.pointerId); } catch { /* capture may already be released */ }
    finishDrag(event.clientX, event.clientY);
  };
  const startMouseDrag = (event: ReactMouseEvent<HTMLElement>, id: StatusBarItemId) => {
    if (event.button !== 0 || !beginDrag(id)) return;
    event.preventDefault();
    const handleMove = (moveEvent: MouseEvent) => {
      moveEvent.preventDefault();
      updateDrag(moveEvent.clientX, moveEvent.clientY);
    };
    const cleanup = () => {
      window.removeEventListener("mousemove", handleMove);
      window.removeEventListener("mouseup", handleUp);
    };
    const handleUp = (upEvent: MouseEvent) => {
      upEvent.preventDefault();
      cleanup();
      mouseDragCleanupRef.current = null;
      finishDrag(upEvent.clientX, upEvent.clientY);
    };
    window.addEventListener("mousemove", handleMove);
    window.addEventListener("mouseup", handleUp);
    mouseDragCleanupRef.current = cleanup;
  };
  const renderRow = (id: StatusBarItemId, visible: boolean, index: number) => {
    const label = itemLabel(id);
    const moveUpLabel = t("settings.statusBarItem.moveUp", { label });
    const moveDownLabel = t("settings.statusBarItem.moveDown", { label });
    const targetPlacement = dragTarget?.id === id ? dragTarget.placement : null;
    return (
      <div
        className={[
          "status-bar-item-row",
          visible ? "" : "status-bar-item-row--hidden",
          draggingItem === id ? "status-bar-item-row--dragging" : "",
          targetPlacement ? "status-bar-item-row--drag-over" : "",
          targetPlacement === "before" ? "status-bar-item-row--drop-before" : "",
          targetPlacement === "after" ? "status-bar-item-row--drop-after" : "",
        ].filter(Boolean).join(" ")}
        data-statusbar-setting-item={id}
        key={id}
      >
        <Tooltip label={t("settings.statusBarItem.drag", { label })}>
          <button
            type="button"
            className="status-bar-item-row__drag"
            disabled={!visible || busy}
            aria-label={t("settings.statusBarItem.drag", { label })}
            onPointerDown={(event) => startPointerDrag(event, id)}
            onPointerMove={movePointerDrag}
            onPointerUp={endPointerDrag}
            onPointerCancel={() => cancelDrag()}
            onMouseDown={(event) => startMouseDrag(event, id)}
          >
            <GripVertical size={14} aria-hidden="true" />
          </button>
        </Tooltip>
        <span className="status-bar-item-row__position" aria-hidden="true">{visible ? index + 1 : "—"}</span>
        <label className="status-bar-item-row__toggle">
          <input
            type="checkbox"
            checked={visible}
            disabled={busy || (visible && visibleItems.length <= 1)}
            onChange={() => toggleItem(id)}
          />
          <span className="status-bar-item-row__check" aria-hidden="true">{visible && <Check size={12} />}</span>
          <span className="status-bar-item-row__label">{label}</span>
        </label>
        <div className="status-bar-item-row__actions">
          <Tooltip label={moveUpLabel}>
            <button type="button" className="status-bar-item-row__order" disabled={busy || !visible || index <= 0} onClick={() => moveItem(id, -1)} aria-label={moveUpLabel}>
              <ChevronUp size={14} aria-hidden="true" />
            </button>
          </Tooltip>
          <Tooltip label={moveDownLabel}>
            <button type="button" className="status-bar-item-row__order" disabled={busy || !visible || index >= visibleItems.length - 1} onClick={() => moveItem(id, 1)} aria-label={moveDownLabel}>
              <ChevronDown size={14} aria-hidden="true" />
            </button>
          </Tooltip>
        </div>
      </div>
    );
  };

  return (
    <div className={`status-bar-items-editor${expanded ? " status-bar-items-editor--expanded" : ""}`}>
      <div className="status-bar-items-editor__summary">
        <span className="status-bar-items-editor__summary-text">
          {t("settings.statusBarItemsSummary", { visible: visibleItems.length, total: DEFAULT_STATUS_BAR_ITEMS.length })}
        </span>
        <div className="status-bar-items-editor__summary-actions">
          {expanded && (
            <>
              <button type="button" className="status-bar-items-editor__action status-bar-items-editor__action--accent" disabled={busy || hiddenItems.length === 0} onClick={() => onChange([...visibleItems, ...hiddenItems])}>
                {t("settings.statusBarItemsShowAll")}
              </button>
              <button type="button" className="status-bar-items-editor__action" disabled={busy || isDefault} onClick={() => onChange([...DEFAULT_STATUS_BAR_ITEMS])}>
                {t("settings.statusBarItemsRestoreDefault")}
              </button>
            </>
          )}
          <Tooltip label={t(expanded ? "settings.statusBarItemsCollapse" : "settings.statusBarItemsExpand")}>
            <button type="button" className="status-bar-items-editor__toggle" aria-expanded={expanded} aria-controls={panelId} aria-label={t(expanded ? "settings.statusBarItemsCollapse" : "settings.statusBarItemsExpand")} onClick={() => setExpanded((open) => !open)}>
              {expanded ? <ChevronUp size={15} aria-hidden="true" /> : <ChevronDown size={15} aria-hidden="true" />}
            </button>
          </Tooltip>
        </div>
      </div>
      {expanded && (
        <div className="status-bar-items-editor__workspace" id={panelId}>
          <section className="status-bar-items-editor__pane status-bar-items-editor__pane--visible" aria-label={visiblePaneLabel}>
            <div className="status-bar-items-editor__pane-title">{visiblePaneLabel}</div>
            <div className="status-bar-items-editor__list">{visibleItems.map((id, index) => renderRow(id, true, index))}</div>
          </section>
          <section
            className={`status-bar-items-editor__pane status-bar-items-editor__pane--hidden${dropZone === "hidden" ? " status-bar-items-editor__pane--drop-target" : ""}`}
            aria-label={hiddenPaneLabel}
            data-statusbar-drop-zone="hidden"
          >
            <div className="status-bar-items-editor__pane-title">{hiddenPaneLabel}</div>
            {hiddenItems.length > 0 ? (
              <div className="status-bar-items-editor__list">{hiddenItems.map((id, index) => renderRow(id, false, index))}</div>
            ) : (
              <div className="status-bar-items-editor__empty" role="status">
                <EyeOff size={28} strokeWidth={1.5} aria-hidden="true" />
                <strong>{t("settings.statusBarItemsHiddenEmpty")}</strong>
                <span>{t("settings.statusBarItemsHiddenEmptyHint")}</span>
              </div>
            )}
          </section>
        </div>
      )}
    </div>
  );
}
