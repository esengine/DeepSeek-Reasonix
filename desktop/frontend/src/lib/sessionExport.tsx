import { createRoot, type Root } from "react-dom/client";
import ReactMarkdown, { defaultUrlTransform } from "react-markdown";
import type { Components } from "react-markdown";
import rehypeKatex from "rehype-katex";
import katexCss from "katex/dist/katex.min.css?inline";
import remarkGfm from "remark-gfm";
import remarkMath from "remark-math";
import { highlightToHtml } from "./highlight";
import { normalizeMath } from "../components/mathNormalize";
import {
  createRasterPdf,
  isSafeInlineExportImage,
  neutralizeExternalCssResources,
  PDF_CONTENT_ASPECT,
  planRasterSlices,
  transformExportMarkdownUrl,
  type RasterSlice,
} from "./sessionExportCore";

const EXPORT_WIDTH = 920;
const MAX_CANVAS_SIDE = 8192;

const EXPORT_STYLES = neutralizeExternalCssResources(`
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
.session-export-page .export-media-placeholder {
  display: inline-block;
  max-width: 100%;
  margin: 4px 0;
  padding: 7px 10px;
  border: 1px solid var(--border-soft);
  border-radius: 6px;
  color: var(--fg-faint);
  background: var(--bg-soft);
  font-size: 0.9em;
}
`);

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
  img: ({ src, alt, title }) => {
    if (isSafeInlineExportImage(src)) {
      return <img src={src} alt={alt ?? ""} title={title} />;
    }
    return <span className="export-media-placeholder">[{alt || title || "image"}]</span>;
  },
};

function StaticMarkdown({ text }: { text: string }) {
  return (
    <div className="md">
      <ReactMarkdown
        remarkPlugins={[remarkGfm, remarkMath]}
        rehypePlugins={[rehypeKatex]}
        components={staticMarkdownComponents}
        urlTransform={(value, key) => transformExportMarkdownUrl(value, key, defaultUrlTransform)}
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

async function waitForInlineImages(surface: HTMLElement): Promise<void> {
  await Promise.all(
    Array.from(surface.querySelectorAll("img")).map(async (image) => {
      if (typeof image.decode === "function") {
        try {
          await image.decode();
        } catch {
          // Broken inline data stays as the browser's normal missing-image UI.
        }
        return;
      }
      if (image.complete) return;
      await new Promise<void>((resolve) => {
        const done = () => {
          image.removeEventListener("load", done);
          image.removeEventListener("error", done);
          resolve();
        };
        image.addEventListener("load", done, { once: true });
        image.addEventListener("error", done, { once: true });
        if (image.complete) done();
      });
    }),
  );
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

  await waitForInlineImages(surface);
  await nextFrame();

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

function replaceExternalMedia(root: HTMLElement): void {
  root.querySelectorAll("img, video, audio, iframe, object, embed").forEach((element) => {
    if (element instanceof HTMLImageElement && isSafeInlineExportImage(element.getAttribute("src") ?? undefined)) return;
    const placeholder = document.createElement("span");
    placeholder.className = "export-media-placeholder";
    const label = element.getAttribute("alt") || element.getAttribute("title") || element.tagName.toLowerCase();
    placeholder.textContent = `[${label}]`;
    element.replaceWith(placeholder);
  });
}

function pruneSliceClone(source: HTMLElement, clone: HTMLElement, slice: RasterSlice): void {
  const sourceMarkdown = source.querySelector<HTMLElement>(".md");
  const cloneMarkdown = clone.querySelector<HTMLElement>(".md");
  if (!sourceMarkdown || !cloneMarkdown) return;
  const surfaceTop = source.getBoundingClientRect().top;
  const markdownTop = sourceMarkdown.getBoundingClientRect().top;
  const selected: Array<{ node: Element; top: number }> = [];
  Array.from(sourceMarkdown.children).forEach((node) => {
    const rect = node.getBoundingClientRect();
    const top = rect.top - surfaceTop;
    const bottom = rect.bottom - surfaceTop;
    if (bottom > slice.offset && top < slice.offset + slice.height) {
      selected.push({ node, top: rect.top - markdownTop });
    }
  });
  if (selected.length === 0) return;

  cloneMarkdown.replaceChildren();
  cloneMarkdown.style.position = "relative";
  cloneMarkdown.style.height = `${Math.max(1, sourceMarkdown.scrollHeight)}px`;
  for (const item of selected) {
    const child = item.node.cloneNode(true) as HTMLElement;
    child.style.position = "absolute";
    child.style.top = `${item.top}px`;
    child.style.left = "0";
    child.style.right = "0";
    child.style.margin = "0";
    cloneMarkdown.appendChild(child);
  }
}

function serializeSurfaceSlice(surface: HTMLElement, width: number, slice: RasterSlice): string {
  const clone = surface.cloneNode(true) as HTMLElement;
  pruneSliceClone(surface, clone, slice);
  replaceExternalMedia(clone);
  clone.setAttribute("xmlns", "http://www.w3.org/1999/xhtml");
  clone.style.width = `${width}px`;
  clone.style.minHeight = `${slice.offset + slice.height}px`;
  clone.style.transform = `translateY(-${slice.offset}px)`;
  clone.style.transformOrigin = "top left";
  const style = document.createElement("style");
  style.textContent = EXPORT_STYLES;
  clone.insertBefore(style, clone.firstChild);
  const viewport = document.createElement("div");
  viewport.setAttribute("xmlns", "http://www.w3.org/1999/xhtml");
  viewport.style.width = `${width}px`;
  viewport.style.height = `${slice.height}px`;
  viewport.style.overflow = "hidden";
  viewport.style.background = "#ffffff";
  viewport.appendChild(clone);
  return new XMLSerializer().serializeToString(viewport);
}

function exportScale(): number {
  return Math.min(2, Math.max(1.5, window.devicePixelRatio || 1));
}

function naturalPageBreaks(surface: HTMLElement): number[] {
  const surfaceTop = surface.getBoundingClientRect().top;
  const markdown = surface.querySelector<HTMLElement>(".md");
  if (!markdown) return [];
  return Array.from(markdown.children).map((node) => Math.ceil(node.getBoundingClientRect().bottom - surfaceTop));
}

function planSurfaceSlices(surface: HTMLElement, maxSliceHeight: number): RasterSlice[] {
  const height = Math.max(1, Math.ceil(surface.scrollHeight || surface.getBoundingClientRect().height || 1));
  const breakpoints = naturalPageBreaks(surface);
  const contentEnd = breakpoints.length > 0
    ? breakpoints.reduce((maximum, value) => Math.max(maximum, value), 1)
    : height;
  return planRasterSlices(height, maxSliceHeight, breakpoints, contentEnd);
}

async function renderSurfaceSliceToCanvas(
  surface: HTMLElement,
  slice: RasterSlice,
  scale: number,
): Promise<HTMLCanvasElement> {
  const width = Math.max(1, Math.ceil(surface.scrollWidth || surface.getBoundingClientRect().width || EXPORT_WIDTH));
  const canvasWidth = Math.max(1, Math.floor(width * scale));
  const canvasHeight = Math.max(1, Math.floor(slice.height * scale));
  const serialized = serializeSurfaceSlice(surface, width, slice);
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${slice.height}" viewBox="0 0 ${width} ${slice.height}"><foreignObject width="100%" height="100%">${serialized}</foreignObject></svg>`;
  // Chromium/WebView2 versions used by shipped desktop builds mark an SVG
  // foreignObject loaded from a blob URL as cross-origin. A self-contained data
  // URL keeps the image origin-clean so toBlob() can encode the canvas.
  const svgUrl = `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`;
  const image = await loadImage(svgUrl);
  const canvas = document.createElement("canvas");
  canvas.width = canvasWidth;
  canvas.height = canvasHeight;
  const ctx = canvas.getContext("2d");
  if (!ctx) throw new Error("Canvas is not available");
  ctx.fillStyle = "#ffffff";
  ctx.fillRect(0, 0, canvasWidth, canvasHeight);
  ctx.scale(scale, scale);
  ctx.drawImage(image, 0, 0, width, slice.height);
  return canvas;
}

function arrayBufferFromBytes(bytes: Uint8Array): ArrayBuffer {
  const copy = new Uint8Array(bytes.byteLength);
  copy.set(bytes);
  return copy.buffer;
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

async function renderSessionImages<T>(markdown: string, encode: (blob: Blob) => Promise<T>): Promise<T[]> {
  const rendered = await renderExportSurface(markdown);
  try {
    const scale = exportScale();
    const slices = planSurfaceSlices(rendered.surface, Math.floor(MAX_CANVAS_SIDE / scale));
    const images: T[] = [];
    for (const slice of slices) {
      const canvas = await renderSurfaceSliceToCanvas(rendered.surface, slice, scale);
      try {
        images.push(await encode(await canvasToBlob(canvas, "image/png")));
      } finally {
        canvas.width = 1;
        canvas.height = 1;
      }
    }
    return images;
  } finally {
    disposeExport(rendered);
  }
}

export function renderSessionImageBlobs(markdown: string): Promise<Blob[]> {
  return renderSessionImages(markdown, async (blob) => blob);
}

export function renderSessionImageBase64Payloads(markdown: string): Promise<string[]> {
  return renderSessionImages(markdown, blobToBase64);
}

export async function renderSessionPdfBlob(markdown: string, title: string): Promise<Blob> {
  const rendered = await renderExportSurface(markdown);
  try {
    const scale = exportScale();
    const width = Math.max(1, Math.ceil(rendered.surface.scrollWidth || rendered.surface.getBoundingClientRect().width || EXPORT_WIDTH));
    const pageHeight = Math.max(1, Math.floor(width * PDF_CONTENT_ASPECT));
    const slices = planSurfaceSlices(rendered.surface, pageHeight);
    const images = [];
    for (const slice of slices) {
      const canvas = await renderSurfaceSliceToCanvas(rendered.surface, slice, scale);
      try {
        const jpegBlob = await canvasToBlob(canvas, "image/jpeg", 0.9);
        images.push({ bytes: new Uint8Array(await jpegBlob.arrayBuffer()), width: canvas.width, height: canvas.height });
      } finally {
        canvas.width = 1;
        canvas.height = 1;
      }
    }
    const pdfBytes = createRasterPdf(images, title);
    return new Blob([arrayBufferFromBytes(pdfBytes)], { type: "application/pdf" });
  } finally {
    disposeExport(rendered);
  }
}
