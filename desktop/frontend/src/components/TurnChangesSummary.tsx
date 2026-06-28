import { useState } from "react";
import { ChevronDown, ChevronRight, FileDiff } from "lucide-react";
import { useT } from "../lib/i18n";
import type { TurnChangeSummary } from "../lib/turnChanges";
import { DiffView } from "./DiffView";

export function TurnChangesSummary({ summary }: { summary: TurnChangeSummary }) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const meta = t("turnChanges.meta", { files: summary.files.length, added: summary.added, removed: summary.removed });
  return (
    <div className={`turn-changes${open ? " turn-changes--open" : ""}`}>
      <button
        type="button"
        className="turn-changes__toggle"
        aria-expanded={open}
        onClick={() => setOpen((value) => !value)}
      >
        {open ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
        <FileDiff size={14} />
        <span className="turn-changes__title">{t("turnChanges.title")}</span>
        <span className="turn-changes__meta">{meta}</span>
      </button>
      {open && (
        <div className="turn-changes__body">
          {summary.files.map((file) => (
            <div className="turn-changes__file" key={file.path}>
              <div className="turn-changes__file-head">
                <span className="turn-changes__path" title={file.path}>{file.path}</span>
                <span className="turn-changes__file-meta">{t("turnChanges.fileMeta", { added: file.added, removed: file.removed })}</span>
              </div>
              {file.patches.map((patch) => (
                <div className="turn-changes__patch" key={patch.id}>
                  <div className="turn-changes__patch-meta">
                    {t("turnChanges.patchMeta", { tool: patch.tool, added: patch.diff.added, removed: patch.diff.removed })}
                  </div>
                  {patch.diff.diff.trim() ? (
                    <DiffView diff={patch.diff.diff} language={patch.language} maxHeight={260} />
                  ) : (
                    <div className="turn-changes__diff-note">{t("turnChanges.diffUnavailable")}</div>
                  )}
                </div>
              ))}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
