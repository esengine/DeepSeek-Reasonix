import { useEffect, useId, useLayoutEffect, useRef, useState, type FormEvent } from "react";
import { createPortal } from "react-dom";
import { useT } from "../lib/i18n";

export function blankProjectNameProblem(name: string): "required" | "invalid" | null {
  const trimmed = name.trim();
  if (!trimmed) return "required";
  if (trimmed === "." || trimmed === ".." || /[\\/]/.test(trimmed) || /[\u0000-\u001f\u007f]/.test(trimmed)) {
    return "invalid";
  }
  return null;
}

export function BlankProjectDialog({
  parentDirectory,
  createdPath,
  busy,
  error,
  onSubmit,
  onCancel,
}: {
  parentDirectory: string;
  createdPath?: string;
  busy: boolean;
  error?: string;
  onSubmit: (name: string) => void;
  onCancel: () => void;
}) {
  const t = useT();
  const titleId = useId();
  const errorId = useId();
  const inputRef = useRef<HTMLInputElement>(null);
  const dialogRef = useRef<HTMLDivElement>(null);
  const restoreFocusRef = useRef<HTMLElement | null>(null);
  const [name, setName] = useState("");
  const problem = blankProjectNameProblem(name);
  const visibleError = error || "";

  useLayoutEffect(() => {
    restoreFocusRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    inputRef.current?.focus();
    return () => {
      if (restoreFocusRef.current?.isConnected) restoreFocusRef.current.focus();
    };
  }, []);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape" && !busy) {
        event.preventDefault();
        event.stopPropagation();
        onCancel();
        return;
      }
      if (event.key !== "Tab") return;
      const focusable = Array.from(dialogRef.current?.querySelectorAll<HTMLElement>(
        'input:not(:disabled), button:not(:disabled)',
      ) ?? []);
      if (focusable.length === 0) return;
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    };
    document.addEventListener("keydown", onKeyDown, { capture: true });
    return () => document.removeEventListener("keydown", onKeyDown, { capture: true });
  }, [busy, onCancel]);

  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (busy || (!createdPath && problem)) return;
    onSubmit(name.trim());
  };

  return createPortal(
    <div
      className="modal-backdrop blank-project-backdrop"
      role="presentation"
      onMouseDown={(event) => {
        if (!busy && event.target === event.currentTarget) onCancel();
      }}
    >
      <div
        ref={dialogRef}
        className="modal blank-project-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
      >
        <form onSubmit={submit}>
          <div className="modal__title" id={titleId}>{t("projectTree.createBlankProject")}</div>
          <div className="blank-project-dialog__field">
            <span className="blank-project-dialog__label">{t("memory.showStorage")}</span>
            <code className="blank-project-dialog__path">{parentDirectory}</code>
          </div>
          <label className="blank-project-dialog__field">
            <span className="blank-project-dialog__label">{t("caps.name")}</span>
            <input
              ref={inputRef}
              className="mem-input blank-project-dialog__input"
              type="text"
              value={name}
              disabled={busy || Boolean(createdPath)}
              aria-invalid={Boolean(visibleError && !createdPath)}
              aria-describedby={visibleError && !createdPath ? errorId : undefined}
              autoComplete="off"
              onChange={(event) => setName(event.target.value)}
            />
          </label>
          {createdPath ? (
            <p className="blank-project-dialog__created">
              {t("memory.createdSkillSuggestion")}: {createdPath}
            </p>
          ) : null}
          <div className="blank-project-dialog__error" id={errorId} role={visibleError ? "alert" : undefined}>
            {visibleError || "\u00a0"}
          </div>
          <div className="modal__actions blank-project-dialog__actions">
            <button className="btn btn--small" type="button" disabled={busy} onClick={onCancel}>
              {t("common.cancel")}
            </button>
            <button className="btn btn--small btn--primary" type="submit" disabled={busy || (!createdPath && Boolean(problem))}>
              {busy ? t("common.loading") : createdPath ? t("common.retry") : t("common.confirm")}
            </button>
          </div>
        </form>
      </div>
    </div>,
    document.body,
  );
}
