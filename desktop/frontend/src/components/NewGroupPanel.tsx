// NewGroupPanel: a small inline dialog to create a project group (#9222). A
// separate control from "new project" so the two actions stay independent.

import { useEffect, useRef } from "react";
import { FolderInput, X } from "lucide-react";

import { useT } from "../lib/i18n";

export function NewGroupPanel({
  open,
  onClose,
  onConfirm,
}: {
  open: boolean;
  onClose: () => void;
  onConfirm: (title: string) => void;
}) {
  const t = useT();
  const inputRef = useRef<HTMLInputElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (open) requestAnimationFrame(() => inputRef.current?.focus());
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

  const confirm = () => {
    const title = inputRef.current?.value.trim();
    if (!title) return;
    onConfirm(title);
    onClose();
  };

  return (
    <div className="move-group-panel move-group-panel--new" ref={panelRef} role="dialog" aria-label={t("projectGroup.createNew")}>
      <div className="move-group__header">
        <FolderInput size={15} aria-hidden="true" />
        <span className="move-group__title">{t("projectGroup.createNew")}</span>
        <button type="button" className="topicbar__action-btn topicbar__action-btn--icon" aria-label={t("common.close")} onClick={onClose}>
          <X size={15} aria-hidden="true" />
        </button>
      </div>
      <div className="move-group__create">
        <input
          ref={inputRef}
          className="mem-input move-group__input"
          placeholder={t("projectGroup.createPlaceholder")}
          aria-label={t("projectGroup.createNew")}
          onKeyDown={(e) => { if (e.key === "Enter") confirm(); }}
        />
        <button type="button" className="btn btn--small" onClick={confirm}>
          {t("projectGroup.createConfirm")}
        </button>
      </div>
    </div>
  );
}

export default NewGroupPanel;
