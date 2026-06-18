import { useRef } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
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
  const scrollRef = useRef<HTMLDivElement>(null);

  const isVirtual = rows.length > 200;

  const virtualizer = useVirtualizer({
    count: isVirtual ? rows.length : 0,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => 24,
    overscan: 10,
  });

  const renderRow = (r: typeof rows[0], idx: number) => (
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
  );

  return (
    <div
      ref={scrollRef}
      className="diff hljs"
      style={{
        maxHeight: maxHeight || undefined,
        overflow: (maxHeight || isVirtual) ? "auto" : undefined,
        position: (maxHeight || isVirtual) ? "relative" : undefined,
      }}
    >
      {isVirtual ? (
        <div
          className="diff__table"
          style={{
            height: virtualizer.getTotalSize(),
            width: "100%",
            position: "relative",
          }}
        >
          {virtualizer.getVirtualItems().map((row) => {
            const item = rows[row.index];
            if (!item) return null;
            return (
              <div
                key={row.key}
                data-index={row.index}
                ref={virtualizer.measureElement}
                style={{
                  position: "absolute",
                  top: 0,
                  left: 0,
                  width: "100%",
                  transform: `translateY(${row.start}px)`,
                }}
              >
                {renderRow(item, row.index)}
              </div>
            );
          })}
        </div>
      ) : (
        <div className="diff__table">
          {rows.map((r, idx) => renderRow(r, idx))}
        </div>
      )}
    </div>
  );
}
