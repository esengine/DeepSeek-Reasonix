import { useState } from "react";
import { ChevronRight } from "lucide-react";
import { useT } from "../lib/i18n";
import { visibleTranscriptMemoryCitations } from "../lib/memoryCitationVisibility";
import type { MemoryCitation } from "../lib/types";

export function MemoryCitations({ citations }: { citations?: MemoryCitation[] }) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const clean = visibleTranscriptMemoryCitations(citations)
    .filter((citation) => (citation.source ?? citation.id ?? citation.note ?? "").trim() !== "")
    .slice(0, 5);
  if (clean.length === 0) return null;
  return (
    <div className="msg-memory-citations">
      <button
        type="button"
        className="msg-memory-citations__toggle"
        aria-expanded={open}
        onClick={() => setOpen((value) => !value)}
      >
        <ChevronRight className={`msg-memory-citations__chevron${open ? " msg-memory-citations__chevron--open" : ""}`} size={15} />
        <span>{t("msg.memoryCompilerCitationsCount", { n: clean.length })}</span>
      </button>
      {open && (
        <div className="msg-memory-citations__body">
          {clean.map((citation, index) => {
            const lines = memoryCitationLines(citation, t);
            return (
              <div key={`${citation.id ?? citation.source}-${index}`} className="msg-memory-citations__item">
                <div className="msg-memory-citations__source">
                  <span>{memoryCitationSource(citation)}</span>
                  {lines && <span className="msg-memory-citations__lines">{lines}</span>}
                </div>
                {citation.note && <div className="msg-memory-citations__note">{citation.note}</div>}
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}

function memoryCitationSource(citation: MemoryCitation): string {
  const source = (citation.source || citation.id || "Memory v5").trim();
  if (citation.kind === "compiler_reference" && source === "Memory v5") return "Memory v5 compiler";
  return source;
}

function memoryCitationLines(citation: MemoryCitation, t: ReturnType<typeof useT>): string {
  const start = citation.lineStart ?? 0;
  const end = citation.lineEnd ?? 0;
  if (start <= 0) return "";
  if (end > 0 && end !== start) return t("msg.memoryCitationLineRange", { start, end });
  return t("msg.memoryCitationLine", { line: start });
}
