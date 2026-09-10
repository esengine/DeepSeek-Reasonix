// MoveToGroupPanel: a small callable panel (independent of ContextMenu, which
// has no submenu support) to move a project into a group. Lists existing
// groups, an "ungrouped" option, and a "new group" entry. Single-select: pick
// a group (or ungrouped) and the call returns immediately (#9222).

import { useEffect, useRef, useState } from "react";
import { FolderInput, FolderPlus, X } from "lucide-react";

import { useT } from "../lib/i18n";
import type { ProjectGroup } from "../lib/projectGroups";

export function MoveToGroupPanel({
  open,
  onClose,
  projectLabel,
  groups,
  currentGroupId,
  onMove,
  onCreateGroup,
}: {
  open: boolean;
  onClose: () => void;
  projectLabel: string;
  groups: ProjectGroup[];
  currentGroupId: string | null;
  onMove: (groupId: string | null) => void;
  onCreateGroup: (title: string) => void;
}) {
  const t = useT();
  const [title, setTitle] = useState("");
  const [creating, setCreating] = useState(false);
  const panelRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (open) {
      setCreating(false);
      setTitle("");
      requestAnimationFrame(() => inputRef.current?.focus());
    }
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    const onPointerDown = (event: MouseEvent) => {
      if (panelRef.current && !panelRef.current.contains(event.target as Node)) onClose();
    };
    document.addEventListener("keydown", onKeyDown);
    document.addEventListener("mousedown", onPointerDown);
    return () => {
      document.removeEventListener("keydown", onKeyDown);
      document.removeEventListener("mousedown", onPointerDown);
    };
  }, [open, onClose]);

  if (!open) return null;

  const pick = (groupId: string | null) => {
    onMove(groupId);
    onClose();
  };
  const confirmCreate = () => {
    const name = title.trim();
    if (!name) return;
    onCreateGroup(name);
    // After creating, move into that group is handled by the caller via
    // onMove; simplest: create group then leave selection to the parent.
    onClose();
  };

  return (
    <div className="move-group-panel" ref={panelRef} role="dialog" aria-label={t("projectGroup.movePanelLabel")}>
      <div className="move-group__header">
        <FolderInput size={15} aria-hidden="true" />
        <span className="move-group__title">
          {t("projectGroup.moveInto")} <em className="move-group__project">{projectLabel}</em>
        </span>
        <button type="button" className="topicbar__action-btn topicbar__action-btn--icon" aria-label={t("common.close")} onClick={onClose}>
          <X size={15} aria-hidden="true" />
        </button>
      </div>
      {!creating ? (
        <div className="move-group__list">
          <button type="button" className="move-group__row" onClick={() => pick(null)}>
            <span className="move-group__swatch move-group__swatch--empty" aria-hidden="true" />
            <span className="move-group__row-label">{currentGroupId === null ? "• " : ""}{t("projectGroup.ungrouped")}</span>
          </button>
          {groups.map((group) => (
            <button
              type="button"
              key={group.id}
              className="move-group__row"
              onClick={() => pick(group.id)}
            >
              <span className="move-group__swatch" aria-hidden="true" />
              <span className="move-group__row-label">{currentGroupId === group.id ? "• " : ""}{group.title}</span>
            </button>
          ))}
          <button type="button" className="move-group__new" onClick={() => setCreating(true)}>
            <FolderPlus size={14} aria-hidden="true" />
            <span>{t("projectGroup.createNew")}</span>
          </button>
        </div>
      ) : (
        <div className="move-group__create">
          <input
            ref={inputRef}
            className="mem-input move-group__input"
            value={title}
            placeholder={t("projectGroup.createPlaceholder")}
            aria-label={t("projectGroup.createNew")}
            onChange={(e) => setTitle(e.target.value)}
            onKeyDown={(e) => { if (e.key === "Enter") confirmCreate(); }}
          />
          <button type="button" className="btn btn--small" disabled={!title.trim()} onClick={confirmCreate}>
            {t("projectGroup.createConfirm")}
          </button>
        </div>
      )}
    </div>
  );
}

export default MoveToGroupPanel;
