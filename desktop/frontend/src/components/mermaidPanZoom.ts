// Inline pan/zoom for the Mermaid preview, replacing svg-pan-zoom (#8068):
// that library inverts the live SVG matrix on wheel/drag and crashes with a
// singular matrix on hidden or zero-sized layouts. Everything here is a plain
// translate/scale string applied to one viewport <g> — no matrix inversion.
//
// Scale is relative to the fit scale (1 = diagram fits the container); pan is
// in screen pixels. The SVG keeps width/height 100% with the browser's own
// viewBox "meet" mapping, which already fits and centers the drawing, so
// fit/center simply reset the user transform.

export interface MermaidPanZoomOptions {
  minZoom?: number;
  maxZoom?: number;
  zoomScaleSensitivity?: number;
}

export interface MermaidPanZoomInstance {
  destroy(): void;
  resize(): void;
  fit(): void;
  center(): void;
  zoomIn(): void;
  zoomOut(): void;
  reset(): void;
}

const VIEWPORT_ATTR = "data-mermaid-pan-zoom-viewport";
const SVG_NS = "http://www.w3.org/2000/svg";

type ViewBox = { x: number; y: number; w: number; h: number };

function readViewBox(svg: SVGSVGElement): ViewBox | null {
  const raw = svg.getAttribute("viewBox");
  if (!raw) return null;
  const parts = raw.trim().split(/[\s,]+/).map(Number);
  if (parts.length !== 4 || parts.some((n) => !Number.isFinite(n)) || parts[2] <= 0 || parts[3] <= 0) return null;
  return { x: parts[0], y: parts[1], w: parts[2], h: parts[3] };
}

function ensureViewport(svg: SVGSVGElement): SVGGElement {
  const existing = svg.querySelector<SVGGElement>(`g[${VIEWPORT_ATTR}]`);
  if (existing) return existing;
  const viewport = document.createElementNS(SVG_NS, "g");
  viewport.setAttribute(VIEWPORT_ATTR, "");
  // Keep definitions in the SVG coordinate system. Mermaid commonly puts
  // markers, clip paths, and gradients in <defs>; moving them under the
  // transformed viewport changes their user coordinate system and can distort
  // arrowheads or clipped/filled shapes.
  for (const child of Array.from(svg.childNodes)) {
    if (child.nodeType === 3 || (child.nodeType === 1 && (child as Element).tagName.toLowerCase() === "defs")) continue;
    viewport.appendChild(child);
  }
  svg.appendChild(viewport);
  return viewport;
}

// Geometry of the browser's meet mapping: drawing units → client pixels,
// including the centering offset inside the SVG's layout box.
function measure(svg: SVGSVGElement): { fitScale: number; originX: number; originY: number } | null {
  const box = readViewBox(svg);
  if (!box) return null;
  const rect = svg.getBoundingClientRect();
  if (rect.width <= 0 || rect.height <= 0) return null;
  const fitScale = Math.min(rect.width / box.w, rect.height / box.h);
  return {
    fitScale,
    originX: rect.left + (rect.width - box.w * fitScale) / 2 - box.x * fitScale,
    originY: rect.top + (rect.height - box.h * fitScale) / 2 - box.y * fitScale,
  };
}

export function createMermaidPanZoom(svg: SVGSVGElement, options: MermaidPanZoomOptions = {}): MermaidPanZoomInstance {
  const minZoom = options.minZoom ?? 0.3;
  const maxZoom = options.maxZoom ?? 8;
  const sensitivity = options.zoomScaleSensitivity ?? 0.3;
  const clampZoom = (zoom: number) => Math.min(maxZoom, Math.max(minZoom, zoom));

  const viewport = ensureViewport(svg);
  let scale = 1;
  let panX = 0;
  let panY = 0;

  // Rounded to 0.1px/0.01% precision so long zoom chains keep clean transforms.
  const num = (n: number) => Math.round(n * 1e4) / 1e4;

  const apply = () => {
    const m = measure(svg);
    if (!m) return;
    viewport.setAttribute("transform", `translate(${num(panX / m.fitScale)} ${num(panY / m.fitScale)}) scale(${num(scale)})`);
  };

  // Keep the content point under (clientX, clientY) fixed while zooming.
  const zoomAt = (clientX: number, clientY: number, nextZoom: number) => {
    const next = clampZoom(nextZoom);
    if (next === scale) return;
    const m = measure(svg);
    if (m) {
      const ratio = next / scale;
      panX = (clientX - m.originX) * (1 - ratio) + panX * ratio;
      panY = (clientY - m.originY) * (1 - ratio) + panY * ratio;
    }
    scale = next;
    apply();
  };

  const center = () => {
    const rect = svg.getBoundingClientRect();
    return { x: rect.left + rect.width / 2, y: rect.top + rect.height / 2 };
  };

  const onWheel = (event: WheelEvent) => {
    event.preventDefault();
    zoomAt(event.clientX, event.clientY, scale * (event.deltaY < 0 ? 1 + sensitivity : 1 / (1 + sensitivity)));
  };

  let drag: { pointerId: number; x: number; y: number; panX: number; panY: number } | null = null;
  const onPointerDown = (event: PointerEvent) => {
    if (event.button !== 0) return;
    drag = { pointerId: event.pointerId, x: event.clientX, y: event.clientY, panX, panY };
    try {
      svg.setPointerCapture(event.pointerId);
    } catch {
      /* pointer capture is unavailable in some test DOMs */
    }
  };
  const onPointerMove = (event: PointerEvent) => {
    if (!drag || event.pointerId !== drag.pointerId) return;
    panX = drag.panX + event.clientX - drag.x;
    panY = drag.panY + event.clientY - drag.y;
    apply();
  };
  const onPointerEnd = (event: PointerEvent) => {
    if (drag?.pointerId === event.pointerId) drag = null;
  };
  const onDblClick = (event: MouseEvent) => {
    event.preventDefault();
    zoomAt(event.clientX, event.clientY, scale * (1 + sensitivity));
  };

  svg.addEventListener("wheel", onWheel, { passive: false });
  svg.addEventListener("pointerdown", onPointerDown);
  svg.addEventListener("pointermove", onPointerMove);
  svg.addEventListener("pointerup", onPointerEnd);
  svg.addEventListener("pointercancel", onPointerEnd);
  svg.addEventListener("dblclick", onDblClick);

  return {
    destroy() {
      svg.removeEventListener("wheel", onWheel);
      svg.removeEventListener("pointerdown", onPointerDown);
      svg.removeEventListener("pointermove", onPointerMove);
      svg.removeEventListener("pointerup", onPointerEnd);
      svg.removeEventListener("pointercancel", onPointerEnd);
      svg.removeEventListener("dblclick", onDblClick);
      drag = null;
    },
    resize() {
      apply();
    },
    fit() {
      scale = clampZoom(1);
      panX = 0;
      panY = 0;
      apply();
    },
    center() {
      panX = 0;
      panY = 0;
      apply();
    },
    zoomIn() {
      const c = center();
      zoomAt(c.x, c.y, scale * (1 + sensitivity));
    },
    zoomOut() {
      const c = center();
      zoomAt(c.x, c.y, scale / (1 + sensitivity));
    },
    reset() {
      scale = 1;
      panX = 0;
      panY = 0;
      apply();
    },
  };
}
