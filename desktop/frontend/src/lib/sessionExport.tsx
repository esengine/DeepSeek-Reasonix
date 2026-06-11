import { createRoot, type Root } from "react-dom/client";
import ReactMarkdown from "react-markdown";
import type { Components } from "react-markdown";
import rehypeKatex from "rehype-katex";
import katexCss from "katex/dist/katex.min.css?inline";
import remarkGfm from "remark-gfm";
import remarkMath from "remark-math";
import { highlightToHtml } from "./highlight";
import { normalizeMath } from "../components/mathNormalize";

const EXPORT_WIDTH = 920;
const MAX_CANVAS_SIDE = 16384;
const PDF_PAGE_WIDTH = 595.28;
const PDF_PAGE_HEIGHT = 841.89;
const PDF_MARGIN = 36;

const EXPORT_STYLES = `
${katexCss}
.session-export-page,
.session-export-page * {
  box-sizing: border-box;
}
.session-export-page {
  --fg: #111827;
  --fg-dim: #374151;
  --fg-faint: #6b7280;
  --bg: #ffffff;
  --bg-soft: #f7f8fb;
  --bg-elev-2: #eef2f7;
  --border: #d8dee8;
  --border-soft: #e7ebf1;
  --accent: #2563eb;
  --font-content: 15px;
  --font-code: 13px;
  --mono: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace;
  width: ${EXPORT_WIDTH}px;
  min-height: 1px;
  padding: 48px 56px;
  background: #ffffff;
  color: var(--fg);
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "Noto Sans SC", "Microsoft YaHei", sans-serif;
  font-size: 16px;
  line-height: 1.68;
  overflow-wrap: anywhere;
}
.session-export-page .md > :first-child {
  margin-top: 0;
}
.session-export-page .md > :last-child {
  margin-bottom: 0;
}
.session-export-page .md p {
  margin: 0 0 12px;
}
.session-export-page .md h1,
.session-export-page .md h2,
.session-export-page .md h3,
.session-export-page .md h4 {
  margin: 20px 0 10px;
  line-height: 1.3;
  font-weight: 650;
  letter-spacing: 0;
}
.session-export-page .md h1 {
  font-size: 1.65em;
  padding-bottom: 14px;
  border-bottom: 1px solid var(--border-soft);
}
.session-export-page .md h2 {
  font-size: 1.28em;
}
.session-export-page .md h3 {
  font-size: 1.12em;
}
.session-export-page .md h4 {
  font-size: 1em;
  color: var(--fg-dim);
}
.session-export-page .md ul,
.session-export-page .md ol {
  margin: 0 0 12px;
  padding-left: 24px;
}
.session-export-page .md li {
  margin: 3px 0;
}
.session-export-page .md li::marker {
  color: var(--fg-faint);
}
.session-export-page .md a {
  color: var(--accent);
  text-decoration: none;
}
.session-export-page .md blockquote {
  margin: 0 0 14px;
  padding: 4px 14px;
  border-left: 3px solid var(--border);
  color: var(--fg-dim);
  background: #fafbfc;
}
.session-export-page .md hr {
  border: none;
  border-top: 1px solid var(--border);
  margin: 20px 0;
}
.session-export-page .md-code {
  font-family: var(--mono);
  font-size: 0.88em;
  background: var(--bg-elev-2);
  border: 1px solid var(--border-soft);
  border-radius: 5px;
  padding: 1px 5px;
}
.session-export-page .md table {
  width: 100%;
  border-collapse: collapse;
  margin: 0 0 14px;
  font-size: var(--font-content);
  display: table;
  overflow: visible;
}
.session-export-page .md th,
.session-export-page .md td {
  border: 1px solid var(--border);
  padding: 7px 10px;
  text-align: left;
  vertical-align: top;
}
.session-export-page .md th {
  background: var(--bg-soft);
  font-weight: 650;
}
.session-export-page .code-block {
  position: relative;
}
.session-export-page .copybtn,
.session-export-page .code-block__copy {
  display: none !important;
}
.session-export-page .code {
  margin: 10px 0;
  padding: 12px 14px;
  background: var(--bg-soft);
  border: 1px solid var(--border-soft);
  border-radius: 8px;
  font-family: var(--mono);
  font-size: var(--font-code);
  line-height: 1.58;
  overflow: visible;
  white-space: pre-wrap;
  color: var(--fg);
}
.session-export-page .hljs-comment,
.session-export-page .hljs-quote {
  color: #6b7280;
}
.session-export-page .hljs-keyword,
.session-export-page .hljs-selector-tag,
.session-export-page .hljs-literal,
.session-export-page .hljs-section,
.session-export-page .hljs-doctag,
.session-export-page .hljs-type,
.session-export-page .hljs-name,
.session-export-page .hljs-strong {
  color: #7c3aed;
}
.session-export-page .hljs-string,
.session-export-page .hljs-regexp,
.session-export-page .hljs-attribute,
.session-export-page .hljs-meta .hljs-string {
  color: #047857;
}
.session-export-page .hljs-number,
.session-export-page .hljs-symbol,
.session-export-page .hljs-bullet,
.session-export-page .hljs-link,
.session-export-page .hljs-selector-attr,
.session-export-page .hljs-selector-pseudo {
  color: #b45309;
}
.session-export-page .hljs-title,
.session-export-page .hljs-title.function_,
.session-export-page .hljs-section .hljs-title {
  color: #1d4ed8;
}
.session-export-page .hljs-class .hljs-title,
.session-export-page .hljs-title.class_,
.session-export-page .hljs-built_in,
.session-export-page .hljs-attr {
  color: #be123c;
}
`;

const staticMarkdownComponents: Components = {
  pre: ({ children }) => <>{children}</>,
  code: ({ className, children }) => {
    const text = String(children ?? "");
    const match = /language-([\w-]+)/.exec(className ?? "");
    const isBlock = match !== null || text.includes("\n");
    if (!isBlock) return <code className="md-code">{children}</code>;
    return (
      <pre className="code hljs" data-lang={match?.[1]}>
        <code dangerouslySetInnerHTML={{ __html: highlightToHtml(text.replace(/\n$/, ""), match?.[1]) }} />
      </pre>
    );
  },
  a: ({ href, children }) => <a href={href}>{children}</a>,
};

function StaticMarkdown({ text }: { text: string }) {
  return (
    <div className="md">
      <ReactMarkdown
        remarkPlugins={[remarkGfm, remarkMath]}
        rehypePlugins={[rehypeKatex]}
        components={staticMarkdownComponents}
      >
        {normalizeMath(text)}
      </ReactMarkdown>
    </div>
  );
}

function ExportSurface({ markdown }: { markdown: string }) {
  return (
    <>
      <style>{EXPORT_STYLES}</style>
      <div className="session-export-page">
        <StaticMarkdown text={markdown} />
      </div>
    </>
  );
}

interface RenderedExport {
  root: Root;
  host: HTMLDivElement;
  surface: HTMLElement;
}

function nextFrame(): Promise<void> {
  return new Promise((resolve) => requestAnimationFrame(() => resolve()));
}

async function renderExportSurface(markdown: string): Promise<RenderedExport> {
  const host = document.createElement("div");
  host.style.position = "fixed";
  host.style.left = "-100000px";
  host.style.top = "0";
  host.style.width = `${EXPORT_WIDTH}px`;
  host.style.pointerEvents = "none";
  host.style.background = "#ffffff";
  document.body.appendChild(host);

  const root = createRoot(host);
  root.render(<ExportSurface markdown={markdown} />);

  await nextFrame();
  await nextFrame();
  await document.fonts?.ready.catch(() => undefined);
  await nextFrame();

  const surface = host.querySelector<HTMLElement>(".session-export-page");
  if (!surface) {
    root.unmount();
    host.remove();
    throw new Error("Export surface was not rendered");
  }

  return { root, host, surface };
}

function disposeExport(rendered: RenderedExport): void {
  rendered.root.unmount();
  rendered.host.remove();
}

function canvasToBlob(canvas: HTMLCanvasElement, type: string, quality?: number): Promise<Blob> {
  return new Promise((resolve, reject) => {
    canvas.toBlob((blob) => {
      if (blob) resolve(blob);
      else reject(new Error("Could not encode export image"));
    }, type, quality);
  });
}

function loadImage(url: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const img = new Image();
    img.onload = () => resolve(img);
    img.onerror = () => reject(new Error("Could not render export image"));
    img.src = url;
  });
}

function serializeSurface(surface: HTMLElement, width: number, height: number): string {
  const clone = surface.cloneNode(true) as HTMLElement;
  clone.setAttribute("xmlns", "http://www.w3.org/1999/xhtml");
  clone.style.width = `${width}px`;
  clone.style.minHeight = `${height}px`;
  const style = document.createElement("style");
  style.textContent = EXPORT_STYLES;
  clone.insertBefore(style, clone.firstChild);
  return new XMLSerializer().serializeToString(clone);
}

async function renderSurfaceToCanvas(surface: HTMLElement): Promise<HTMLCanvasElement> {
  const width = Math.max(1, Math.ceil(surface.scrollWidth || surface.getBoundingClientRect().width || EXPORT_WIDTH));
  const height = Math.max(1, Math.ceil(surface.scrollHeight || surface.getBoundingClientRect().height || 1));
  const preferredScale = Math.min(2, Math.max(1, window.devicePixelRatio || 1));
  const scale = Math.min(preferredScale, MAX_CANVAS_SIDE / width, MAX_CANVAS_SIDE / height);
  const canvasWidth = Math.max(1, Math.floor(width * scale));
  const canvasHeight = Math.max(1, Math.floor(height * scale));
  const serialized = serializeSurface(surface, width, height);
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}" viewBox="0 0 ${width} ${height}"><foreignObject width="100%" height="100%">${serialized}</foreignObject></svg>`;
  const svgUrl = URL.createObjectURL(new Blob([svg], { type: "image/svg+xml;charset=utf-8" }));

  try {
    const image = await loadImage(svgUrl);
    const canvas = document.createElement("canvas");
    canvas.width = canvasWidth;
    canvas.height = canvasHeight;
    const ctx = canvas.getContext("2d");
    if (!ctx) throw new Error("Canvas is not available");
    ctx.fillStyle = "#ffffff";
    ctx.fillRect(0, 0, canvasWidth, canvasHeight);
    ctx.scale(scale, scale);
    ctx.drawImage(image, 0, 0, width, height);
    return canvas;
  } finally {
    URL.revokeObjectURL(svgUrl);
  }
}

function bytesFromString(value: string): Uint8Array {
  const bytes = new Uint8Array(value.length);
  for (let i = 0; i < value.length; i++) {
    bytes[i] = value.charCodeAt(i) & 0xff;
  }
  return bytes;
}

function concatBytes(chunks: Uint8Array[]): Uint8Array {
  const total = chunks.reduce((sum, chunk) => sum + chunk.length, 0);
  const out = new Uint8Array(total);
  let offset = 0;
  for (const chunk of chunks) {
    out.set(chunk, offset);
    offset += chunk.length;
  }
  return out;
}

function arrayBufferFromBytes(bytes: Uint8Array): ArrayBuffer {
  const copy = new Uint8Array(bytes.byteLength);
  copy.set(bytes);
  return copy.buffer;
}

function pdfNumber(value: number): string {
  return value.toFixed(3).replace(/\.?0+$/, "");
}

function pdfString(value: string): string {
  return value
    .replace(/[^\x20-\x7e]/g, "")
    .replace(/\\/g, "\\\\")
    .replace(/\(/g, "\\(")
    .replace(/\)/g, "\\)");
}

function createRasterPdf(jpegBytes: Uint8Array, imageWidth: number, imageHeight: number, title: string): Uint8Array {
  const contentWidth = PDF_PAGE_WIDTH - PDF_MARGIN * 2;
  const contentHeight = PDF_PAGE_HEIGHT - PDF_MARGIN * 2;
  const renderedHeight = imageHeight * (contentWidth / imageWidth);
  const pageCount = Math.max(1, Math.ceil(renderedHeight / contentHeight));
  const imageObjectId = 3 + pageCount * 2;
  const infoObjectId = imageObjectId + 1;
  const objectCount = infoObjectId;
  const chunks: Uint8Array[] = [];
  const offsets: number[] = new Array(objectCount + 1).fill(0);
  let position = 0;

  const push = (value: string | Uint8Array) => {
    const bytes = typeof value === "string" ? bytesFromString(value) : value;
    chunks.push(bytes);
    position += bytes.length;
  };
  const addObject = (id: number, body: string) => {
    offsets[id] = position;
    push(`${id} 0 obj\n${body}\nendobj\n`);
  };
  const addStreamObject = (id: number, header: string, body: Uint8Array) => {
    offsets[id] = position;
    push(`${id} 0 obj\n${header}\nstream\n`);
    push(body);
    push("\nendstream\nendobj\n");
  };

  push("%PDF-1.4\n%\xff\xff\xff\xff\n");
  addObject(1, "<< /Type /Catalog /Pages 2 0 R >>");

  const kids = Array.from({ length: pageCount }, (_, index) => `${3 + index * 2} 0 R`).join(" ");
  addObject(2, `<< /Type /Pages /Kids [ ${kids} ] /Count ${pageCount} >>`);

  for (let index = 0; index < pageCount; index++) {
    const pageId = 3 + index * 2;
    const contentId = pageId + 1;
    const y = PDF_PAGE_HEIGHT - PDF_MARGIN - renderedHeight + index * contentHeight;
    const stream = bytesFromString(
      `q\n${pdfNumber(contentWidth)} 0 0 ${pdfNumber(renderedHeight)} ${pdfNumber(PDF_MARGIN)} ${pdfNumber(y)} cm\n/Im0 Do\nQ\n`,
    );
    addObject(
      pageId,
      `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 ${pdfNumber(PDF_PAGE_WIDTH)} ${pdfNumber(PDF_PAGE_HEIGHT)}] /Resources << /XObject << /Im0 ${imageObjectId} 0 R >> /ProcSet [/PDF /ImageC] >> /Contents ${contentId} 0 R >>`,
    );
    addStreamObject(contentId, `<< /Length ${stream.length} >>`, stream);
  }

  addStreamObject(
    imageObjectId,
    `<< /Type /XObject /Subtype /Image /Width ${imageWidth} /Height ${imageHeight} /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode /Length ${jpegBytes.length} >>`,
    jpegBytes,
  );
  addObject(infoObjectId, `<< /Title (${pdfString(title)}) /Producer (Reasonix) >>`);

  const xrefStart = position;
  push(`xref\n0 ${objectCount + 1}\n0000000000 65535 f \n`);
  for (let id = 1; id <= objectCount; id++) {
    push(`${String(offsets[id]).padStart(10, "0")} 00000 n \n`);
  }
  push(`trailer\n<< /Size ${objectCount + 1} /Root 1 0 R /Info ${infoObjectId} 0 R >>\nstartxref\n${xrefStart}\n%%EOF\n`);

  return concatBytes(chunks);
}

export function blobToBase64(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      const result = String(reader.result ?? "");
      resolve(result.includes(",") ? result.slice(result.indexOf(",") + 1) : result);
    };
    reader.onerror = () => reject(reader.error ?? new Error("Could not read export payload"));
    reader.readAsDataURL(blob);
  });
}

export async function renderSessionImageBlob(markdown: string): Promise<Blob> {
  const rendered = await renderExportSurface(markdown);
  try {
    const canvas = await renderSurfaceToCanvas(rendered.surface);
    return await canvasToBlob(canvas, "image/png");
  } finally {
    disposeExport(rendered);
  }
}

export async function renderSessionPdfBlob(markdown: string, title: string): Promise<Blob> {
  const rendered = await renderExportSurface(markdown);
  try {
    const canvas = await renderSurfaceToCanvas(rendered.surface);
    const jpegBlob = await canvasToBlob(canvas, "image/jpeg", 0.92);
    const jpegBytes = new Uint8Array(await jpegBlob.arrayBuffer());
    const pdfBytes = createRasterPdf(jpegBytes, canvas.width, canvas.height, title);
    return new Blob([arrayBufferFromBytes(pdfBytes)], { type: "application/pdf" });
  } finally {
    disposeExport(rendered);
  }
}
