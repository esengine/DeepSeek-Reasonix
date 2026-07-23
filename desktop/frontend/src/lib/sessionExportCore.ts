const PDF_PAGE_WIDTH = 595.28;
const PDF_PAGE_HEIGHT = 841.89;
const PDF_MARGIN = 36;

export const PDF_CONTENT_ASPECT =
  (PDF_PAGE_HEIGHT - PDF_MARGIN * 2) / (PDF_PAGE_WIDTH - PDF_MARGIN * 2);

export interface RasterSlice {
  offset: number;
  height: number;
}

export interface RasterPdfImage {
  bytes: Uint8Array;
  width: number;
  height: number;
}

export function planRasterSlices(
  totalHeight: number,
  maxSliceHeight: number,
  naturalBreakpoints: number[] = [],
  contentEnd?: number,
): RasterSlice[] {
  const total = Math.max(1, Math.ceil(Number.isFinite(totalHeight) ? totalHeight : 1));
  const limit = Math.max(1, Math.floor(Number.isFinite(maxSliceHeight) ? maxSliceHeight : 1));
  // contentEnd lets callers distinguish meaningful content from trailing
  // container whitespace. Plan pages through the content first, then retain
  // only the trailing whitespace that still fits on the final content page.
  const plannedTotal = Math.max(
    1,
    Math.min(total, Math.ceil(contentEnd !== undefined && Number.isFinite(contentEnd) ? contentEnd : total)),
  );
  const breakpoints = naturalBreakpoints
    .filter((value) => Number.isFinite(value) && value > 0 && value < plannedTotal)
    .map((value) => Math.floor(value))
    .sort((a, b) => a - b);
  const slices: RasterSlice[] = [];
  let offset = 0;
  while (offset < plannedTotal) {
    const target = Math.min(plannedTotal, offset + limit);
    let end = target;
    if (target < plannedTotal) {
      const earliestNaturalBreak = offset + Math.floor(limit * 0.55);
      for (const breakpoint of breakpoints) {
        if (breakpoint > target) break;
        if (breakpoint >= earliestNaturalBreak) end = breakpoint;
      }
    }
    if (end <= offset) end = target;
    slices.push({ offset, height: end - offset });
    offset = end;
  }
  const last = slices[slices.length - 1];
  if (last && plannedTotal < total && last.height < limit) {
    last.height += Math.min(total - plannedTotal, limit - last.height);
  }
  return slices;
}

// SVG foreignObject rendering becomes origin-tainted when its CSS references a
// font or image URL. Export surfaces use system fonts, so external resources are
// intentionally neutralised before the SVG is drawn onto a canvas.
export function neutralizeExternalCssResources(css: string): string {
  return css.replace(/url\(\s*(?:"[^"]*"|'[^']*'|[^)]*)\s*\)/gi, "none");
}

export function isSafeInlineExportImage(src: string | undefined): boolean {
  return /^data:image\/(?:png|jpe?g|webp|gif);base64,[a-z0-9+/]+={0,2}$/i.test(src?.trim() ?? "");
}

export function transformExportMarkdownUrl(
  value: string,
  key: string,
  fallback: (value: string) => string,
): string {
  const trimmed = value.trim();
  if (key === "src" && isSafeInlineExportImage(trimmed)) return trimmed;
  return fallback(value);
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

export function createRasterPdf(images: RasterPdfImage[], title: string): Uint8Array {
  if (images.length === 0) throw new Error("Cannot create a PDF without pages");

  const contentWidth = PDF_PAGE_WIDTH - PDF_MARGIN * 2;
  const contentHeight = PDF_PAGE_HEIGHT - PDF_MARGIN * 2;
  const infoObjectId = 3 + images.length * 3;
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
  const kids = images.map((_, index) => `${3 + index * 3} 0 R`).join(" ");
  addObject(2, `<< /Type /Pages /Kids [ ${kids} ] /Count ${images.length} >>`);

  images.forEach((image, index) => {
    const pageId = 3 + index * 3;
    const contentId = pageId + 1;
    const imageId = pageId + 2;
    const renderedHeight = Math.min(contentHeight, image.height * (contentWidth / image.width));
    const y = PDF_PAGE_HEIGHT - PDF_MARGIN - renderedHeight;
    const stream = bytesFromString(
      `q\n${pdfNumber(contentWidth)} 0 0 ${pdfNumber(renderedHeight)} ${pdfNumber(PDF_MARGIN)} ${pdfNumber(y)} cm\n/Im0 Do\nQ\n`,
    );
    addObject(
      pageId,
      `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 ${pdfNumber(PDF_PAGE_WIDTH)} ${pdfNumber(PDF_PAGE_HEIGHT)}] /Resources << /XObject << /Im0 ${imageId} 0 R >> /ProcSet [/PDF /ImageC] >> /Contents ${contentId} 0 R >>`,
    );
    addStreamObject(contentId, `<< /Length ${stream.length} >>`, stream);
    addStreamObject(
      imageId,
      `<< /Type /XObject /Subtype /Image /Width ${image.width} /Height ${image.height} /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode /Length ${image.bytes.length} >>`,
      image.bytes,
    );
  });

  addObject(infoObjectId, `<< /Title (${pdfString(title)}) /Producer (Reasonix) >>`);
  const xrefStart = position;
  push(`xref\n0 ${objectCount + 1}\n0000000000 65535 f \n`);
  for (let id = 1; id <= objectCount; id++) {
    push(`${String(offsets[id]).padStart(10, "0")} 00000 n \n`);
  }
  push(`trailer\n<< /Size ${objectCount + 1} /Root 1 0 R /Info ${infoObjectId} 0 R >>\nstartxref\n${xrefStart}\n%%EOF\n`);
  return concatBytes(chunks);
}
