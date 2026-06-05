import type { DiffProps } from "../DiffView";
import { diffLines } from "../../lib/diff";
import { highlightToHtml } from "../../lib/highlight";

// HljsDiff is the syntax-highlighted default behind the diff seam: an LCS line
// diff with a +/- gutter, each line highlighted in the target language. A real
// editor (Monaco DiffEditor / CodeMirror merge) would replace this via
// DiffView.tsx's lazy import.
const SIGN: Record<"ctx" | "add" | "del", string> = { ctx: " ", add: "+", del: "-" };

export default function HljsDiff({ original, modified, language, maxHeight }: DiffProps) {
  const rows = diffLines(original, modified);
  return (
    <div className="diff hljs" style={maxHeight ? { maxHeight } : undefined}>
      {rows.map((r, idx) => (
        <div key={idx} className={`diff__row diff__row--${r.type}`}>
          <span className="diff__sign">{SIGN[r.type]}</span>
          <code
            className="diff__text"
            dangerouslySetInnerHTML={{ __html: highlightToHtml(r.text, language) }}
          />
        </div>
      ))}
    </div>
  );
}
