import { useEffect, useRef, useState } from "react";
import { t } from "../../i18n";
import type { DocumentViewState } from "./viewState";

export default function DelimitedPreview({ text, path, view }: { text: string; path: string; view: DocumentViewState }) {
  const [result, setResult] = useState<{ text: string; rows?: string[][]; error?: boolean }>();
  const [top, setTop] = useState(view.top ?? 0);
  const [left, setLeft] = useState(view.left ?? 0);
  const scroll = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (scroll.current) { scroll.current.scrollTop = view.top ?? 0; scroll.current.scrollLeft = view.left ?? 0; }
  }, [result, view]);
  useEffect(() => {
    const worker = new Worker(new URL("./delimited.worker.ts", import.meta.url), { type: "module" });
    const timer = window.setTimeout(() => { worker.terminate(); setResult({ text, error: true }); }, 10_000);
    worker.onmessage = (event: MessageEvent<{ rows?: string[][]; error?: boolean }>) => {
      window.clearTimeout(timer);
      setResult({ text, ...event.data });
      worker.terminate();
    };
    worker.onerror = () => { window.clearTimeout(timer); setResult({ text, error: true }); worker.terminate(); };
    worker.postMessage({ text, delimiter: /\.tsv$/i.test(path) ? "\t" : "," });
    return () => { window.clearTimeout(timer); worker.terminate(); };
  }, [text, path]);
  if (result?.text !== text) return <p role="status">{t("common.loading")}</p>;
  if (result.error || !result.rows) return <p role="alert">{t("chat.the_table_is_invalid_or")}</p>;
  const rows = result.rows, height = 30;
  const start = Math.min(Math.max(0, rows.length - 1), Math.max(0, Math.floor(top / height) - 10));
  const visible = rows.slice(start, start + 80);
  const columns = rows.reduce((n, row) => Math.max(n, row.length), 0);
  const firstColumn = Math.min(Math.max(0, columns - 1), Math.max(0, Math.floor(left / 160) - 2));
  const count = Math.min(20, columns - firstColumn);
  return <div className="document-table" ref={scroll} data-action="document.scroll" data-target={path} onScroll={(e) => {
    view.top = e.currentTarget.scrollTop; view.left = e.currentTarget.scrollLeft;
    setTop(view.top); setLeft(view.left);
  }}>
    <table aria-label={path} aria-rowcount={rows.length} aria-colcount={columns} style={{ width: columns * 160 + 48, tableLayout: "fixed" }}><colgroup>
      <col style={{ width: 48 }} />
      {firstColumn > 0 && <col style={{ width: firstColumn * 160 }} />}
      {Array.from({ length: count }, (_, i) => <col key={i} style={{ width: 160 }} />)}
      {columns > firstColumn + count && <col style={{ width: (columns - firstColumn - count) * 160 }} />}
    </colgroup><tbody>
      <tr aria-hidden="true" style={{ height: start * height }}><td /></tr>
      {visible.map((row, i) => <tr key={start + i} aria-rowindex={start + i + 1} style={{ height }}>
        <th scope="row">{start + i + 1}</th>
        {firstColumn > 0 && <td aria-hidden="true" />}
        {Array.from({ length: count }, (_, col) => <td key={col} aria-colindex={firstColumn + col + 1} title={row[firstColumn + col] ?? ""}>{row[firstColumn + col] ?? ""}</td>)}
        {columns > firstColumn + count && <td aria-hidden="true" />}
      </tr>)}
      <tr aria-hidden="true" style={{ height: Math.max(0, rows.length - start - visible.length) * height }}><td /></tr>
    </tbody></table>
  </div>;
}
