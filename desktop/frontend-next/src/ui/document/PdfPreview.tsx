import { useEffect, useRef, useState } from "react";
import { getDocument, GlobalWorkerOptions, TextLayer, type PDFDocumentProxy } from "pdfjs-dist";
import workerURL from "pdfjs-dist/build/pdf.worker.min.mjs?url";
import "pdfjs-dist/web/pdf_viewer.css";
import { t } from "../../i18n";
import type { DocumentViewState } from "./viewState";

GlobalWorkerOptions.workerSrc = workerURL;

export default function PdfPreview({ data, view }: { data: ArrayBuffer; view: DocumentViewState }) {
  const [doc, setDoc] = useState<PDFDocumentProxy>();
  const [page, setPage] = useState(view.page ?? 1);
  const [zoom, setZoom] = useState(view.zoom ?? 0);
  useEffect(() => { view.page = page; view.zoom = zoom; }, [view, page, zoom]);
  const [width, setWidth] = useState(600);
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
  return <div className="document-pdf">
    <div className="document-controls">
      <button data-action="document.page" disabled={page <= 1 || !doc} onClick={() => setPage((n) => n - 1)}>{t("common.previous_page")}</button>
      <span>{page} / {doc?.numPages ?? "—"}</span>
      <button data-action="document.page" disabled={!doc || page >= doc.numPages} onClick={() => setPage((n) => n + 1)}>{t("common.next_page")}</button>
      <select data-action="document.zoom" aria-label={t("chat.zoom")} value={zoom} onChange={(e) => setZoom(Number(e.target.value))}>
        <option value={0}>{t("chat.fit_width")}</option>
        {[0.5, 1, 1.5, 2].map((n) => <option key={n} value={n}>{n * 100}%</option>)}
      </select>
    </div>
    {error && <p role="alert">{t("chat.cannot_display_this_pdf_it")}</p>}
    {!doc && !error && <p role="status">{t("common.loading")}</p>}
    <div className="document-pdf-scroll" ref={frame}>
      <div className="document-pdf-page" hidden={!doc || error}>
        <canvas ref={canvas} /><div ref={text} className="textLayer" />
      </div>
    </div>
  </div>;
}
