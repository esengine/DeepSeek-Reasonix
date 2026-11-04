import { useEffect, useRef, useState } from "react";
import { getDocument, GlobalWorkerOptions, TextLayer, type PDFDocumentProxy } from "pdfjs-dist";
import workerURL from "pdfjs-dist/build/pdf.worker.min.mjs?url";
import "pdfjs-dist/web/pdf_viewer.css";
import { t } from "../../i18n";
import { StudioIcon } from "../StudioIcon";
import type { DocumentViewState } from "./viewState";

GlobalWorkerOptions.workerSrc = workerURL;

const STEPS = [0.5, 0.75, 1, 1.25, 1.5, 2, 3];

export default function PdfPreview({ data, view, onOpenExternal }: { data: ArrayBuffer; view: DocumentViewState; onOpenExternal?: () => void }) {
  const [doc, setDoc] = useState<PDFDocumentProxy>();
  const [page, setPage] = useState(view.page ?? 1);
  const [zoom, setZoom] = useState(view.zoom ?? 0);
  useEffect(() => { view.page = page; view.zoom = zoom; }, [view, page, zoom]);
  const [width, setWidth] = useState(600);
  // The scale the page was last drawn at, which is what fit-width means in percent.
  const [drawn, setDrawn] = useState(1);
  const [typed, setTyped] = useState<string | null>(null);
  const [error, setError] = useState(false);
  const frame = useRef<HTMLDivElement>(null);
  const canvas = useRef<HTMLCanvasElement>(null);
  const text = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!frame.current) return;
    const observer = new ResizeObserver(([entry]) => setWidth(Math.max(100, entry.contentRect.width - 24)));
    observer.observe(frame.current);
    return () => observer.disconnect();
  }, []);
  useEffect(() => {
    let live = true;
    setDoc(undefined); setError(false);
    const assets = new URL("./pdfjs/", document.baseURI).href;
    // PDF.js 6 removed eval and isEvalSupported (mozilla/pdf.js@f6bac014).
    // This reader renders only canvas/text; it never creates a scripting manager.
    const task = getDocument({
      data: data.slice(0),
      enableXfa: false,
      cMapUrl: assets + "cmaps/", cMapPacked: true,
      standardFontDataUrl: assets + "standard_fonts/", wasmUrl: assets + "wasm/",
    });
    task.onPassword = () => { if (live) setError(true); void task.destroy(); };
    void task.promise.then((next) => {
      if (live) { setDoc(next); setPage((n) => Math.min(n, next.numPages)); }
    }).catch(() => { if (live) setError(true); });
    return () => { live = false; void task.destroy(); };
  }, [data]);
  useEffect(() => {
    if (!doc || !canvas.current || !text.current) return;
    let live = true;
    let cancel = () => {};
    let layer: TextLayer | undefined;
    const surface = canvas.current, overlay = text.current;
    void doc.getPage(page).then(async (pdfPage) => {
      if (!live) return;
      const base = pdfPage.getViewport({ scale: 1 });
      const scale = zoom || width / base.width;
      setDrawn(scale);
      const viewport = pdfPage.getViewport({ scale });
      const ratio = Math.min(window.devicePixelRatio || 1, Math.sqrt(16_777_216 / (viewport.width * viewport.height)));
      surface.width = Math.max(1, Math.floor(viewport.width * ratio));
      surface.height = Math.max(1, Math.floor(viewport.height * ratio));
      surface.style.width = `${viewport.width}px`;
      surface.style.height = `${viewport.height}px`;
      overlay.replaceChildren();
      overlay.style.setProperty("--scale-factor", String(scale));
      overlay.style.setProperty("--total-scale-factor", String(scale));
      overlay.style.width = `${viewport.width}px`;
      overlay.style.height = `${viewport.height}px`;
      const render = pdfPage.render({ canvas: surface, viewport, transform: [ratio, 0, 0, ratio, 0, 0] });
      cancel = () => render.cancel();
      layer = new TextLayer({ textContentSource: pdfPage.streamTextContent(), container: overlay, viewport });
      await Promise.all([render.promise, layer.render()]);
    }).catch(() => { if (live) setError(true); });
    return () => { live = false; cancel(); layer?.cancel(); };
  }, [doc, page, zoom, width]);
  const pages = doc?.numPages ?? 0;
  const go = (raw: string) => {
    setTyped(null);
    const n = Number.parseInt(raw, 10);
    if (Number.isFinite(n) && pages) setPage(Math.min(pages, Math.max(1, n)));
  };
  const step = (dir: 1 | -1) => {
    const next = dir > 0 ? STEPS.find((n) => n > drawn + 0.01) : [...STEPS].reverse().find((n) => n < drawn - 0.01);
    setZoom(next ?? (dir > 0 ? STEPS.at(-1)! : STEPS[0]));
  };
  if (error) {
    return (
      <div className="pdf-fail" role="alert">
        <StudioIcon name="warning" />
        <span>{t("chat.cannot_display_this_pdf_it")}</span>
        {onOpenExternal && <button type="button" data-action="workbench.open-file" onClick={onOpenExternal}>{t("chat.open_in_system_application")}</button>}
      </div>
    );
  }
  return <div className="document-pdf">
    <div className="pdf-bar">
      <button type="button" className="pdf-icon" data-action="document.page" disabled={page <= 1 || !doc} aria-label={t("common.previous_page")} title={t("common.previous_page")}
        onClick={() => setPage((n) => n - 1)}><StudioIcon name="chevron" className="pdf-back" /></button>
      <span className="pdf-count">
        <input data-action-keydown="document.page" data-action-change="document.page" data-action-blur="document.page" inputMode="numeric" aria-label={t("chat.page_number")} disabled={!doc}
          value={typed ?? String(page)} onChange={(e) => setTyped(e.target.value)}
          onKeyDown={(e) => { if (e.key === "Enter") go(e.currentTarget.value); else if (e.key === "Escape") setTyped(null); }}
          onBlur={(e) => go(e.currentTarget.value)} />
        / {pages || "—"}
      </span>
      <button type="button" className="pdf-icon" data-action="document.page" disabled={!doc || page >= pages} aria-label={t("common.next_page")} title={t("common.next_page")}
        onClick={() => setPage((n) => n + 1)}><StudioIcon name="chevron" /></button>
      <span className="pdf-fill" />
      <button type="button" className="pdf-icon" data-action="document.zoom" disabled={!doc} aria-label={t("chat.zoom_out")} title={t("chat.zoom_out")} onClick={() => step(-1)}><StudioIcon name="minus" /></button>
      <span className="pdf-zoom">{Math.round(drawn * 100)}%</span>
      <button type="button" className="pdf-icon" data-action="document.zoom" disabled={!doc} aria-label={t("chat.zoom_in")} title={t("chat.zoom_in")} onClick={() => step(1)}><StudioIcon name="plus" /></button>
      <button type="button" className="pdf-fit" data-action="document.zoom" aria-pressed={zoom === 0} disabled={!doc} onClick={() => setZoom(0)}>
        <StudioIcon name="expand" />{t("chat.fit_width")}
      </button>
    </div>
    {!doc && <p role="status">{t("common.loading")}</p>}
    <div className="document-pdf-scroll" ref={frame}>
      <div className="document-pdf-page" hidden={!doc}>
        <canvas ref={canvas} /><div ref={text} className="textLayer" />
      </div>
    </div>
  </div>;
}
