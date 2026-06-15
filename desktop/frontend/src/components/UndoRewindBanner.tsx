import { useState } from "react";
import { useT } from "../lib/i18n";

export interface UndoRewindMeta {
  /** How many conversation turns were lost. */
  turns: number;
  /** File paths that were restored (code rewind). */
  filesRestored: string[];
  /** File paths that were removed (code rewind). */
  filesRemoved: string[];
  /** Callback when the user confirms undo. */
  onUndo: () => void;
}

export function UndoRewindBanner({ meta }: { meta: UndoRewindMeta }) {
  const t = useT();
  const [confirm, setConfirm] = useState(false);

  const parts: string[] = [];
  if (meta.turns > 0) parts.push(t("undoRewind.turns", { n: meta.turns }));
  if (meta.filesRestored.length > 0)
    parts.push(t("undoRewind.filesRestored", { n: meta.filesRestored.length }));
  if (meta.filesRemoved.length > 0)
    parts.push(t("undoRewind.filesRemoved", { n: meta.filesRemoved.length }));
  const summary = parts.join(" · ");

  const files = [...new Set([...meta.filesRestored, ...meta.filesRemoved])];
  const fileList = files.length > 0 ? files.slice(0, 3).map((f) => f.split(/[/\\]/).pop() || f).join(", ") + (files.length > 3 ? ` +${files.length - 3}` : "") : "";

  return (
    <div className="undo-rewind">
      <div className="undo-rewind__info">
        <span className="undo-rewind__label">{summary}</span>
        {fileList && <span className="undo-rewind__files">{fileList}</span>}
      </div>
      <button
        type="button"
        className={`undo-rewind__btn${confirm ? " undo-rewind__btn--confirm" : ""}`}
        onClick={() => {
          if (confirm) {
            meta.onUndo();
            setConfirm(false);
          } else {
            setConfirm(true);
          }
        }}
      >
        {confirm ? t("undoRewind.confirm") : t("undoRewind.undo")}
      </button>
    </div>
  );
}
