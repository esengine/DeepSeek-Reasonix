// MarkdownTable — components-map override for markdown tables. Small tables
// render as-is; tables with more than MARKDOWN_TABLE_VIRTUAL_MIN_ROWS body
// rows mount through @tanstack/react-virtual inside a bounded-height scroll
// container (same idiom as the transcript list), so a thousand-row tool dump
// cannot monopolize a frame. Content fidelity is kept: every row is in the
// virtual model, just not mounted until scrolled to.

import { Children, cloneElement, isValidElement, memo, useRef, type CSSProperties, type ReactElement, type ReactNode } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import type { MarkdownTableAlignment, VirtualMarkdownTableData } from "../lib/largeMarkdownTable";

export const MARKDOWN_TABLE_VIRTUAL_MIN_ROWS = 50;
const VIRTUAL_TABLE_MAX_HEIGHT = 480;
const ESTIMATED_ROW_HEIGHT = 36;
const VIRTUAL_TABLE_OVERSCAN = 12;

function findTablePart(children: ReactNode, tag: "thead" | "tbody"): ReactElement | null {
  for (const child of Children.toArray(children)) {
    if (isValidElement(child) && child.type === tag) return child as ReactElement;
  }
  return null;
}

function tableRows(tbody: ReactElement | null): ReactElement[] {
  if (!tbody) return [];
  const children = (tbody.props as { children?: ReactNode }).children;
  return Children.toArray(children).filter(
    (child): child is ReactElement => isValidElement(child) && child.type === "tr",
  );
}

const VirtualMarkdownTable = memo(function VirtualMarkdownTable({
  head,
  rows,
}: {
  head: ReactElement | null;
  rows: ReactElement[];
}) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const virtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => ESTIMATED_ROW_HEIGHT,
    overscan: VIRTUAL_TABLE_OVERSCAN,
    // First paint before layout measurement (and jsdom tests, where measured
    // rects are zero) still virtualizes against a real viewport height.
    initialRect: { width: 640, height: VIRTUAL_TABLE_MAX_HEIGHT },
  });
  return (
    <div ref={scrollRef} className="md-table-virtual" style={{ maxHeight: VIRTUAL_TABLE_MAX_HEIGHT }}>
      <table>
        {head}
        <tbody style={{ height: virtualizer.getTotalSize(), position: "relative" }}>
          {virtualizer.getVirtualItems().map((virtualRow) => {
            const row = rows[virtualRow.index];
            if (!row) return null;
            return cloneElement(row, {
              key: row.key ?? virtualRow.key,
              "data-index": virtualRow.index,
              ref: virtualizer.measureElement,
              style: {
                position: "absolute",
                top: 0,
                left: 0,
                width: "100%",
                transform: `translateY(${virtualRow.start}px)`,
              } as CSSProperties,
            } as Partial<unknown>);
          })}
        </tbody>
      </table>
    </div>
  );
});

export const MarkdownTable = memo(function MarkdownTable({ children }: { children?: ReactNode }) {
  const tbody = findTablePart(children, "tbody");
  const rows = tableRows(tbody);
  if (rows.length <= MARKDOWN_TABLE_VIRTUAL_MIN_ROWS) return <table>{children}</table>;
  return <VirtualMarkdownTable head={findTablePart(children, "thead")} rows={rows} />;
});

function alignmentProps(align: MarkdownTableAlignment): { align?: "left" | "center" | "right" } {
  return align ? { align } : {};
}

/** Virtual table used by the worker's conservative large plain-table path. */
export const VirtualMarkdownSourceTable = memo(function VirtualMarkdownSourceTable({
  data,
}: {
  data: VirtualMarkdownTableData;
}) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const virtualizer = useVirtualizer({
    count: data.rows.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => ESTIMATED_ROW_HEIGHT,
    overscan: VIRTUAL_TABLE_OVERSCAN,
    initialRect: { width: 640, height: VIRTUAL_TABLE_MAX_HEIGHT },
  });
  return (
    <div
      ref={scrollRef}
      className="md-table-virtual"
      data-markdown-source-rows={data.rows.length}
      style={{ maxHeight: VIRTUAL_TABLE_MAX_HEIGHT }}
    >
      <table>
        <thead>
          <tr>
            {data.header.map((cell, index) => (
              <th key={index} {...alignmentProps(data.align[index] ?? null)}>{cell}</th>
            ))}
          </tr>
        </thead>
        <tbody style={{ height: virtualizer.getTotalSize(), position: "relative" }}>
          {virtualizer.getVirtualItems().map((virtualRow) => {
            const row = data.rows[virtualRow.index];
            if (!row) return null;
            return (
              <tr
                key={virtualRow.key}
                data-index={virtualRow.index}
                ref={virtualizer.measureElement}
                style={{
                  position: "absolute",
                  top: 0,
                  left: 0,
                  width: "100%",
                  transform: `translateY(${virtualRow.start}px)`,
                }}
              >
                {row.map((cell, index) => (
                  <td key={index} {...alignmentProps(data.align[index] ?? null)}>{cell}</td>
                ))}
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
});
