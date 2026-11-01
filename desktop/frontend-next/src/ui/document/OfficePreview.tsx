import { useEffect, useMemo, useRef, useState } from "react";
import { t } from "../../i18n";
import { staticDocument } from "./staticDocument";
import { readWorkbook, SHEET_COLUMN_LIMIT, SHEET_ROW_LIMIT, type SheetView } from "./officeSheet";
import { fitZoom, readWordDocument, type WordView } from "./officeWord";
import type { DocumentViewState } from "./viewState";

type Parsed = { kind: "word"; word: WordView } | { kind: "sheets"; sheets: SheetView[] } | { kind: "failed" };

function useWidth() {
  const ref = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(0);
  useEffect(() => {
    const el = ref.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(([entry]) => setWidth(Math.round(entry.contentRect.width)));
    observer.observe(el);
    return () => observer.disconnect();
  }, []);
  return [ref, width] as const;
}

export default function OfficePreview({ data, path, view }: { data: ArrayBuffer; path: string; view: DocumentViewState }) {
  const [parsed, setParsed] = useState<Parsed>();
  const [sheet, updateSheet] = useState(view.sheet ?? 0);
  const setSheet = (i: number) => { view.sheet = i; updateSheet(i); };
  const [box, width] = useWidth();
  const [chosen, updateZoom] = useState(view.zoom);
  const setZoom = (value: number | undefined) => { view.zoom = value; updateZoom(value); };
  useEffect(() => {
    let live = true;
    setParsed(undefined);
    const read = /\.xlsx$/i.test(path)
      ? readWorkbook(data).then((sheets): Parsed => ({ kind: "sheets", sheets }))
      : readWordDocument(data).then((word): Parsed => ({ kind: "word", word }));
    read.catch((): Parsed => ({ kind: "failed" })).then((p) => { if (live) setParsed(p); });
    return () => { live = false; };
  }, [data, path]);
  const current = parsed?.kind === "sheets" ? parsed.sheets[Math.min(sheet, parsed.sheets.length - 1)] : undefined;
  const fit = parsed?.kind === "word" ? fitZoom(width, parsed.word.pageWidth) : 1;
  const zoom = chosen ?? fit;
  const html = parsed?.kind === "word" ? `<style>body{zoom:${zoom}}</style>${parsed.word.html}` : current?.html;
  const doc = useMemo(() => html === undefined ? "" : staticDocument(html), [html]);

  let body;
  if (!parsed) body = <p role="status">{t("正在读取…")}</p>;
  else if (parsed.kind === "failed") body = <p role="alert">{t("无法解析这个文件，可以用系统应用打开")}</p>;
  else if (parsed.kind === "sheets" && !current) body = <p role="status">{t("这个工作簿没有可见的工作表")}</p>;
  else body = <iframe className="document-html document-office" title={t("Office 文档预览")} sandbox="" srcDoc={doc} />;
  return <div className="document-office-view" ref={box}>
    {parsed && parsed.kind !== "failed" && <p className="document-notice">{t("只读预览，宏、脚本和外部资源未加载")}</p>}
    {parsed?.kind === "word" && (
      <div className="document-controls">
        <button data-action="document.zoom" data-target={path} onClick={() => setZoom(Math.max(0.25, zoom - 0.1))} aria-label={t("缩小")}>−</button>
        <button data-action="document.zoom" data-target={path} aria-pressed={chosen === undefined} onClick={() => setZoom(undefined)}>{t("适应宽度")} · {Math.round(zoom * 100)}%</button>
        <button data-action="document.zoom" data-target={path} onClick={() => setZoom(Math.min(3, zoom + 0.1))} aria-label={t("放大")}>+</button>
      </div>
    )}
    {parsed?.kind === "sheets" && parsed.sheets.length > 1 && (
      <div className="document-controls" role="tablist" aria-label={t("工作表")}>
        {parsed.sheets.map((s, i) => (
          <button key={s.name} role="tab" aria-selected={s === current} data-action="document.sheet" data-target={s.name} onClick={() => setSheet(i)}>{s.name}</button>
        ))}
      </div>
    )}
    {current?.clipped && <p className="document-notice">{t("只显示前 {rows} 行、{cols} 列", { rows: SHEET_ROW_LIMIT, cols: SHEET_COLUMN_LIMIT })}</p>}
    {body}
  </div>;
}
