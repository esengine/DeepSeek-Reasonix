import type { DiffProps } from "../DiffView";
import { diffLines, diffRowsFromUnifiedDiff } from "../../lib/diff";
import { highlightToHtml } from "../../lib/highlight";

// HljsDiff is the syntax-highlighted default behind the diff seam: an LCS line
// diff with a +/- gutter, each line highlighted in the target language. A real
// editor (Monaco DiffEditor / CodeMirror merge) would replace this via
// DiffView.tsx's lazy import.
const SIGN: Record<"ctx" | "add" | "del", string> = { ctx: " ", add: "+", del: "-" };

function lineNo(n?: number): string {
  return typeof n === "number" ? String(n) : "";
}

export default function HljsDiff({ original = "", modified = "", diff = "", language, maxHeight }: DiffProps) {
  const rows = diff ? diffRowsFromUnifiedDiff(diff) : diffLines(original, modified);
  return (
    <div className="diff hljs" style={maxHeight ? { maxHeight } : undefined}>
      <div className="diff__table">
        {rows.map((r, idx) => (
          <div key={idx} className={`diff__row diff__row--${r.type}`}>
            <span className="diff__gutter">
              <span className="diff__line diff__line--old">{lineNo(r.oldLine)}</span>
              <span className="diff__line diff__line--new">{lineNo(r.newLine)}</span>
              <span className="diff__sign">{SIGN[r.type]}</span>
            </span>
            <code
              className="diff__text"
              dangerouslySetInnerHTML={{ __html: highlightToHtml(r.text, language) }}
            />
          </div>
        ))}
      </div>
    </div>
  );
}
