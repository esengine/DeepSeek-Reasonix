import { useEffect, useMemo, useRef, useState } from "react";
import { t } from "../../i18n";
import { StudioIcon } from "../StudioIcon";
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
  const html = parsed?.kind === "word" ? `<style>body{zoom:${zoom}}</style>${parsed.word.html}`
    : current && `<style>body{zoom:${zoom}}</style>${current.html}`;
  const doc = useMemo(() => html === undefined ? "" : staticDocument(html), [html]);

  let body;
  if (!parsed) body = <p role="status">{t("common.loading")}</p>;
  else if (parsed.kind === "failed") body = <p role="alert">{t("chat.this_file_could_not_be")}</p>;
  else if (parsed.kind === "sheets" && !current) body = <p role="status">{t("chat.this_workbook_has_no_visible")}</p>;
  else body = <iframe className="document-html document-office" title={t("common.office_document_preview")} sandbox="" srcDoc={doc} />;
  const step = (dir: 1 | -1) => setZoom(Math.min(3, Math.max(0.25, Math.round((zoom + dir * 0.1) * 100) / 100)));
  return <div className="document-office-view" ref={box}>
    {parsed && parsed.kind !== "failed" && (
      <div className="office-head">
        <span className="office-ro">{t("common.read_only")}</span>
        <span className="office-note">{t("chat.read_only_preview_macros_scripts")}</span>
        <span className="office-fill" />
        <button type="button" className="pdf-icon" data-action="document.zoom" data-target={path} aria-label={t("chat.zoom_out")} title={t("chat.zoom_out")} onClick={() => step(-1)}><StudioIcon name="minus" /></button>
        <span className="pdf-zoom">{Math.round(zoom * 100)}%</span>
        <button type="button" className="pdf-icon" data-action="document.zoom" data-target={path} aria-label={t("chat.zoom_in")} title={t("chat.zoom_in")} onClick={() => step(1)}><StudioIcon name="plus" /></button>
        {parsed.kind === "word" && (
          <button type="button" className="pdf-fit" data-action="document.zoom" data-target={path} aria-pressed={chosen === undefined} onClick={() => setZoom(undefined)}>
            <StudioIcon name="expand" />{t("chat.fit_width")}
          </button>
        )}
      </div>
    )}
    {body}
    {parsed?.kind === "sheets" && current && (
      <div className="office-sheets">
        <div role="tablist" aria-label={t("chat.sheets")}>
          {parsed.sheets.map((s, i) => (
            <button key={s.name} type="button" role="tab" aria-selected={s === current} data-action="document.sheet" data-target={s.name} onClick={() => setSheet(i)}>{s.name}</button>
          ))}
        </div>
        <span className="office-fill" />
        {current.clipped && <small>{t("chat.showing_the_first_rows_rows", { rows: SHEET_ROW_LIMIT, cols: SHEET_COLUMN_LIMIT })}</small>}
      </div>
    )}
  </div>;
}
