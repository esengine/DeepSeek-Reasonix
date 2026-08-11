import { memo, type RefObject, useCallback, useEffect, useId, useLayoutEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { AlertCircle, Code2, Maximize2, Minimize2, Play, RotateCcw, ZoomIn, ZoomOut } from "lucide-react";
import { CopyButton } from "./CopyButton";
import { openExternal } from "../lib/bridge";
import { markdownImageSource } from "../lib/markdownImage";

interface MermaidDiagramProps {
  definition: string;
}

type DiagramState =
  | { status: "loading" }
  | { status: "rendered"; svg: string }
  | { status: "error"; message: string };

type DiagramTab = "preview" | "code";
type MermaidThemeName = "dark" | "light";
type MermaidModule = typeof import("mermaid");
type MermaidApi = MermaidModule["default"];
type MermaidRenderAdapter = (
  svgId: string,
  definition: string,
  theme: MermaidThemeName,
  signal: AbortSignal,
) => Promise<string>;

type PanZoomInstance = {
  destroy(): void;
  resize(): unknown;
  fit(): unknown;
  center(): unknown;
  zoomIn(): unknown;
  zoomOut(): unknown;
  reset(): unknown;
};

type PanZoomFactory = (svg: SVGSVGElement, options?: Record<string, unknown>) => PanZoomInstance;

const MAX_TEXT_SIZE = 100000;
const MIN_ZOOM = 0.3;
const MAX_ZOOM = 8;
const SAFE_LINK_PROTOCOLS = new Set(["http:", "https:", "mailto:"]);
const XLINK_NS = "http://www.w3.org/1999/xlink";

let mermaidApi: MermaidApi | null = null;
let initPromise: Promise<MermaidApi> | null = null;
let renderQueue: Promise<void> = Promise.resolve();
let panZoomFactory: PanZoomFactory | null = null;
let panZoomPromise: Promise<PanZoomFactory | null> | null = null;
let renderAdapterForTest: MermaidRenderAdapter | null = null;
let panZoomFactoryForTest: PanZoomFactory | null | undefined;

export function __setMermaidRenderAdapterForTest(adapter: MermaidRenderAdapter | null): void {
  renderAdapterForTest = adapter;
  renderQueue = Promise.resolve();
}

export function __setMermaidPanZoomFactoryForTest(factory: PanZoomFactory | null | undefined): void {
  panZoomFactoryForTest = factory;
}

function mermaidThemeVariables(theme: MermaidThemeName): Record<string, string | number | boolean> {
  if (theme === "dark") {
    return {
      darkMode: true,
      background: "#111319",
      fontSize: "13px",
      fontFamily: "ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif",
      primaryColor: "#1f2937",
      primaryTextColor: "#f4f5f7",
      primaryBorderColor: "#374151",
      mainBkg: "#1f2937",
      nodeBkg: "#1f2937",
      nodeBorder: "#374151",
      nodeTextColor: "#f4f5f7",
      stateBkg: "#1f2937",
      stateBorder: "#374151",
      stateLabelColor: "#f4f5f7",
      labelColor: "#d1d5db",
      lineColor: "#6b7280",
      textColor: "#d1d5db",
      defaultLinkColor: "#6b7280",
      edgeLabelBackground: "#111319",
      clusterBkg: "#0f172a",
      clusterBorder: "#374151",
      actorBkg: "#1f2937",
      actorBorder: "#374151",
      actorTextColor: "#f4f5f7",
      actorLineColor: "#4b5563",
      signalColor: "#d1d5db",
      signalTextColor: "#f4f5f7",
      labelBoxBkgColor: "#1f2937",
      labelBoxBorderColor: "#374151",
      labelTextColor: "#f4f5f7",
      loopTextColor: "#f4f5f7",
      noteBkgColor: "#0f172a",
      noteBorderColor: "#374151",
      noteTextColor: "#e5e7eb",
      classText: "#f4f5f7",
      classBorder: "#374151",
      classBkg: "#1f2937",
      secondaryColor: "#111319",
      tertiaryColor: "#1f2937",
    };
  }
  return {
    darkMode: false,
    background: "#ffffff",
    fontSize: "13px",
    fontFamily: "ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif",
    primaryColor: "#f8fafc",
    primaryTextColor: "#0f172a",
    primaryBorderColor: "#cbd5e1",
    mainBkg: "#f8fafc",
    nodeBkg: "#f8fafc",
    nodeBorder: "#cbd5e1",
    nodeTextColor: "#0f172a",
    stateBkg: "#f8fafc",
    stateBorder: "#cbd5e1",
    stateLabelColor: "#0f172a",
    labelColor: "#475569",
    lineColor: "#94a3b8",
    textColor: "#475569",
    defaultLinkColor: "#94a3b8",
    edgeLabelBackground: "#ffffff",
    clusterBkg: "#f1f5f9",
    clusterBorder: "#e2e8f0",
    actorBkg: "#f8fafc",
    actorBorder: "#cbd5e1",
    actorTextColor: "#0f172a",
    actorLineColor: "#cbd5e1",
    signalColor: "#475569",
    signalTextColor: "#0f172a",
    labelBoxBkgColor: "#f8fafc",
    labelBoxBorderColor: "#cbd5e1",
    labelTextColor: "#0f172a",
    loopTextColor: "#0f172a",
    noteBkgColor: "#f1f5f9",
    noteBorderColor: "#e2e8f0",
    noteTextColor: "#334155",
    classText: "#0f172a",
    classBorder: "#cbd5e1",
    classBkg: "#f8fafc",
    secondaryColor: "#f1f5f9",
    tertiaryColor: "#f8fafc",
  };
}

function mermaidConfigForTheme(theme: MermaidThemeName) {
  return {
    startOnLoad: false,
    theme: "base" as const,
    securityLevel: "antiscript" as const,
    maxTextSize: MAX_TEXT_SIZE,
    fontFamily: "ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif",
    flowchart: { htmlLabels: false },
    themeVariables: mermaidThemeVariables(theme),
  };
}

async function ensureMermaid(theme: MermaidThemeName): Promise<MermaidApi> {
  if (mermaidApi) {
    mermaidApi.initialize(mermaidConfigForTheme(theme));
    return mermaidApi;
  }
  if (!initPromise) {
    initPromise = import("mermaid").then((mod) => {
      mermaidApi = mod.default;
      mermaidApi.initialize(mermaidConfigForTheme(theme));
      return mermaidApi;
    }).catch((err) => {
      initPromise = null;
      mermaidApi = null;
      throw err;
    });
  }
  const api = await initPromise;
  api.initialize(mermaidConfigForTheme(theme));
  return api;
}

function queuedRender(
  svgId: string,
  definition: string,
  theme: MermaidThemeName,
  signal: AbortSignal,
): Promise<string> {
  const promise = renderQueue.then(async () => {
    if (signal.aborted) throw new DOMException("Aborted", "AbortError");
    const api = await ensureMermaid(theme);
    if (signal.aborted) throw new DOMException("Aborted", "AbortError");
    const { svg } = await api.render(svgId, definition);
    if (signal.aborted) throw new DOMException("Aborted", "AbortError");
    return svg;
  });
  renderQueue = promise.then(() => {}, () => {});
  return promise;
}

async function renderMermaid(
  svgId: string,
  definition: string,
  theme: MermaidThemeName,
  signal: AbortSignal,
): Promise<string> {
  if (renderAdapterForTest) return renderAdapterForTest(svgId, definition, theme, signal);
  return queuedRender(svgId, definition, theme, signal);
}

async function ensurePanZoomFactory(): Promise<PanZoomFactory | null> {
  if (panZoomFactoryForTest !== undefined) return panZoomFactoryForTest;
  if (panZoomFactory) return panZoomFactory;
  if (!panZoomPromise) {
    panZoomPromise = import("svg-pan-zoom").then((mod) => {
      const factory = (("default" in mod ? mod.default : mod) as unknown) as PanZoomFactory;
      panZoomFactory = factory;
      return factory;
    }).catch(() => null);
  }
  return panZoomPromise;
}

export function isSafeMermaidHref(href: string | null | undefined): boolean {
  const value = href?.trim();
  if (!value) return false;
  if (value.startsWith("#")) return true;
  try {
    return SAFE_LINK_PROTOCOLS.has(new URL(value).protocol);
  } catch {
    return false;
  }
}

export function isOpenableMermaidHref(href: string | null | undefined): boolean {
  const value = href?.trim();
  return Boolean(value && !value.startsWith("#") && isSafeMermaidHref(value));
}

export function getMermaidAnchorHref(anchor: Element): string | null {
  return anchor.getAttribute("href")
    ?? anchor.getAttribute("xlink:href")
    ?? anchor.getAttributeNS(XLINK_NS, "href");
}

export function sanitizeMermaidSvg(svg: string): string {
  if (typeof DOMParser === "undefined" || typeof XMLSerializer === "undefined") return svg;
  const doc = new DOMParser().parseFromString(svg, "image/svg+xml");
  const svgEl = doc.documentElement;
  if (svgEl.localName.toLowerCase() !== "svg") return svg;

  const elements = [svgEl, ...Array.from(svgEl.querySelectorAll("*"))];
  const toRemove: Element[] = [];

  for (const element of elements) {
    const name = element.localName.toLowerCase();
    if (name === "script" || name === "iframe" || name === "object" || name === "embed") {
      toRemove.push(element);
      continue;
    }
    for (const attr of Array.from(element.attributes)) {
      const attrName = attr.name;
      const localName = attr.localName.toLowerCase();
      if (/^on/i.test(attrName)) {
        element.removeAttribute(attrName);
        continue;
      }
      if (localName === "href") {
        if (!isSafeMermaidHref(attr.value)) {
          element.removeAttribute(attrName);
          continue;
        }
        if (name === "image" || name === "feimage") {
          attr.value = markdownImageSource(attr.value);
        } else if (name === "use" && !attr.value.trim().startsWith("#")) {
          // External <use> references can load arbitrary SVG documents. Mermaid
          // only needs local fragment references, so keep those and drop the rest.
          element.removeAttribute(attrName);
        }
      }
    }
  }

  for (const element of toRemove) element.parentNode?.removeChild(element);
  return new XMLSerializer().serializeToString(svgEl);
}

export function resolveMermaidTheme(): MermaidThemeName {
  if (typeof document === "undefined") return "dark";
  const forced = document.documentElement.getAttribute("data-theme");
  if (forced === "light" || forced === "dark") return forced;
  return window.matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark";
}

function useMermaidTheme(): MermaidThemeName {
  const [theme, setTheme] = useState<MermaidThemeName>(resolveMermaidTheme);

  useEffect(() => {
    const html = document.documentElement;
    const updateTheme = () => setTheme(resolveMermaidTheme());
    const observer = new MutationObserver(updateTheme);
    observer.observe(html, { attributeFilter: ["data-theme", "data-theme-mode"] });

    const mq = window.matchMedia("(prefers-color-scheme: light)");
    mq.addEventListener("change", updateTheme);
    return () => {
      observer.disconnect();
      mq.removeEventListener("change", updateTheme);
    };
  }, []);

  return theme;
}

function destroyPanZoom(instance: PanZoomInstance | null): void {
  if (!instance) return;
  try {
    instance.destroy();
  } catch {
    /* svg-pan-zoom cleanup is best-effort across browser and test DOMs. */
  }
}

function panZoomLayoutReady(container: HTMLElement): boolean {
  const rect = container.getBoundingClientRect();
  return Number.isFinite(rect.width) && Number.isFinite(rect.height) && rect.width > 0 && rect.height > 0;
}

export function safelyRunPanZoom(instance: PanZoomInstance | null, action: () => void): boolean {
  if (!instance) return false;
  try {
    action();
    return true;
  } catch {
    // svg-pan-zoom asks SVGMatrix.inverse() to invert the current transform.
    // Hidden/zero-sized layouts can produce a singular matrix in WebKit and
    // Chromium; keep that library detail out of the global crash surface.
    return false;
  }
}

export function safelySyncPanZoom(instance: PanZoomInstance | null, container: HTMLElement | null): boolean {
  if (!instance || !container || !panZoomLayoutReady(container)) return false;
  return safelyRunPanZoom(instance, () => {
    instance.resize();
    instance.fit();
    instance.center();
  });
}

const MermaidDiagram = memo(function MermaidDiagram({ definition }: MermaidDiagramProps) {
  const [state, setState] = useState<DiagramState>({ status: "loading" });
  const [tab, setTab] = useState<DiagramTab>("preview");
  const [fullscreen, setFullscreen] = useState(false);
  const [portalTarget, setPortalTarget] = useState<Element | null>(null);
  const theme = useMermaidTheme();
  const instanceId = useId().replace(/[^a-zA-Z0-9_-]/g, "-");
  const svgId = `mermaid-${instanceId}`;
  const previewRef = useRef<HTMLDivElement>(null);
  const panZoomRef = useRef<PanZoomInstance | null>(null);
  const mountedRef = useRef(true);
  const source = useMemo(() => definition.replace(/\n$/, ""), [definition]);

  useEffect(() => {
    mountedRef.current = true;
    const controller = new AbortController();
    setState({ status: "loading" });

    (async () => {
      try {
        const trimmed = source.trim();
        if (!trimmed) {
          setState({ status: "error", message: "Empty diagram definition" });
          return;
        }
        const rendered = await renderMermaid(svgId, trimmed, theme, controller.signal);
        if (controller.signal.aborted || !mountedRef.current) return;
        setState({ status: "rendered", svg: sanitizeMermaidSvg(rendered) });
      } catch (err) {
        if (err instanceof DOMException && err.name === "AbortError") return;
        if (controller.signal.aborted || !mountedRef.current) return;
        const message = err instanceof Error ? err.message : String(err);
        setState({ status: "error", message });
      }
    })();

    return () => {
      controller.abort();
      mountedRef.current = false;
    };
  }, [source, svgId, theme]);

  useLayoutEffect(() => {
    if (tab !== "preview" || state.status !== "rendered") return;
    const container = previewRef.current;
    if (!container) return;

    let cancelled = false;
    let raf = 0;
    let syncRaf = 0;
    let resizeObserver: ResizeObserver | null = null;
    let instance: PanZoomInstance | null = null;

    const queueSync = () => {
      if (syncRaf) window.cancelAnimationFrame(syncRaf);
      let attempts = 0;
      const sync = () => {
        syncRaf = 0;
        if (cancelled || panZoomRef.current !== instance) return;
        attempts += 1;
        if (!safelySyncPanZoom(instance, container) && attempts < 4) {
          syncRaf = window.requestAnimationFrame(sync);
        }
      };
      syncRaf = window.requestAnimationFrame(sync);
    };

    void ensurePanZoomFactory().then((factory) => {
      if (cancelled || !factory) return;
      raf = window.requestAnimationFrame(() => {
        const svg = container.querySelector("svg") as SVGSVGElement | null;
        if (cancelled || !svg) return;
        svg.removeAttribute("width");
        svg.removeAttribute("height");
        svg.style.width = "100%";
        svg.style.height = "100%";
        svg.style.maxWidth = "none";

        destroyPanZoom(panZoomRef.current);
        try {
          instance = factory(svg, {
            zoomEnabled: true,
            panEnabled: true,
            controlIconsEnabled: false,
            dblClickZoomEnabled: true,
            // Fitting inside the constructor can run before the portal/layout has
            // dimensions, which is the matrix-inverse crash seen in diagnostics.
            fit: false,
            center: false,
            minZoom: MIN_ZOOM,
            maxZoom: MAX_ZOOM,
            zoomScaleSensitivity: 0.3,
          });
          panZoomRef.current = instance;
          queueSync();
          if (typeof ResizeObserver !== "undefined") {
            resizeObserver = new ResizeObserver(queueSync);
            resizeObserver.observe(container);
          }
        } catch {
          instance = null;
          panZoomRef.current = null;
        }
      });
    });

    return () => {
      cancelled = true;
      if (raf) window.cancelAnimationFrame(raf);
      if (syncRaf) window.cancelAnimationFrame(syncRaf);
      resizeObserver?.disconnect();
      if (panZoomRef.current === instance) panZoomRef.current = null;
      destroyPanZoom(instance);
    };
  }, [state, tab, fullscreen]);

  useEffect(() => {
    if (!fullscreen) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setFullscreen(false);
        setPortalTarget(null);
      }
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [fullscreen]);

  useEffect(() => {
    if (!fullscreen || !panZoomRef.current) return;
    const raf = window.requestAnimationFrame(() => {
      safelySyncPanZoom(panZoomRef.current, previewRef.current);
    });
    return () => window.cancelAnimationFrame(raf);
  }, [fullscreen]);

  const toggleFullscreen = useCallback(() => {
    setFullscreen((current) => {
      const next = !current;
      setPortalTarget(next ? document.querySelector(".chat-pane") ?? document.body : null);
      return next;
    });
  }, []);

  const zoomIn = useCallback(() => {
    const instance = panZoomRef.current;
    safelyRunPanZoom(instance, () => instance?.zoomIn());
  }, []);

  const zoomOut = useCallback(() => {
    const instance = panZoomRef.current;
    safelyRunPanZoom(instance, () => instance?.zoomOut());
  }, []);

  const resetZoom = useCallback(() => {
    const instance = panZoomRef.current;
    if (!instance) return;
    safelyRunPanZoom(instance, () => instance.reset());
    safelySyncPanZoom(instance, previewRef.current);
  }, []);

  const body = (
    <MermaidBody
      state={state}
      source={source}
      tab={tab}
      previewRef={previewRef}
    />
  );

  const content = (
    <div className={[
      "mermaid-diagram",
      state.status === "error" ? "mermaid-diagram--error" : "",
      fullscreen ? "mermaid-diagram--fullscreen" : "",
    ].filter(Boolean).join(" ")}>
      <MermaidToolbar
        tab={tab}
        source={source}
        fullscreen={fullscreen}
        onTabChange={setTab}
        onFullscreenToggle={toggleFullscreen}
        onZoomIn={zoomIn}
        onZoomOut={zoomOut}
        onResetZoom={resetZoom}
      />
      {body}
    </div>
  );

  if (fullscreen && portalTarget) {
    return (
      <>
        <div className="mermaid-diagram mermaid-diagram--placeholder" aria-hidden="true" />
        {createPortal(content, portalTarget)}
      </>
    );
  }

  return content;
});

function MermaidToolbar({
  tab,
  source,
  fullscreen,
  onTabChange,
  onFullscreenToggle,
  onZoomIn,
  onZoomOut,
  onResetZoom,
}: {
  tab: DiagramTab;
  source: string;
  fullscreen: boolean;
  onTabChange: (tab: DiagramTab) => void;
  onFullscreenToggle: () => void;
  onZoomIn: () => void;
  onZoomOut: () => void;
  onResetZoom: () => void;
}) {
  return (
    <div className="mermaid-diagram__toolbar">
      <div className="mermaid-diagram__title" aria-hidden="true">Mermaid</div>
      <div className="mermaid-diagram__actions">
        <button
          type="button"
          className={`mermaid-diagram__icon-btn${tab === "preview" ? " mermaid-diagram__icon-btn--active" : ""}`}
          onClick={() => onTabChange("preview")}
          aria-label="Preview diagram"
          title="Preview diagram"
        >
          <Play size={14} />
        </button>
        <button
          type="button"
          className={`mermaid-diagram__icon-btn${tab === "code" ? " mermaid-diagram__icon-btn--active" : ""}`}
          onClick={() => onTabChange("code")}
          aria-label="Show diagram source"
          title="Show diagram source"
        >
          <Code2 size={14} />
        </button>
        <button
          type="button"
          className="mermaid-diagram__icon-btn mermaid-diagram__zoom-action"
          onClick={onZoomOut}
          aria-label="Zoom out"
          title="Zoom out"
        >
          <ZoomOut size={14} />
        </button>
        <button
          type="button"
          className="mermaid-diagram__icon-btn mermaid-diagram__zoom-action"
          onClick={onZoomIn}
          aria-label="Zoom in"
          title="Zoom in"
        >
          <ZoomIn size={14} />
        </button>
        <button
          type="button"
          className="mermaid-diagram__icon-btn mermaid-diagram__zoom-action"
          onClick={onResetZoom}
          aria-label="Reset zoom"
          title="Reset zoom"
        >
          <RotateCcw size={14} />
        </button>
        <CopyButton text={source} className="mermaid-diagram__copy-btn" showInlineLabel={false} />
        <button
          type="button"
          className="mermaid-diagram__icon-btn"
          onClick={onFullscreenToggle}
          aria-label={fullscreen ? "Exit fullscreen" : "Open fullscreen"}
          title={fullscreen ? "Exit fullscreen" : "Open fullscreen"}
        >
          {fullscreen ? <Minimize2 size={14} /> : <Maximize2 size={14} />}
        </button>
      </div>
    </div>
  );
}

function MermaidBody({
  state,
  source,
  tab,
  previewRef,
}: {
  state: DiagramState;
  source: string;
  tab: DiagramTab;
  previewRef: RefObject<HTMLDivElement | null>;
}) {
  if (tab === "code") {
    return (
      <pre className="code hljs mermaid-diagram__code" data-lang="mermaid">
        <code>{source}</code>
      </pre>
    );
  }

  if (state.status === "loading") {
    return (
      <div className="mermaid-diagram__loading">
        <span className="mermaid-diagram__spinner" />
        <span>Rendering diagram...</span>
      </div>
    );
  }

  if (state.status === "error") {
    return (
      <div className="mermaid-diagram__error">
        <div className="mermaid-diagram__error-bar">
          <AlertCircle size={14} className="mermaid-diagram__error-icon" />
          <span>Diagram syntax error</span>
        </div>
        <pre className="code hljs mermaid-diagram__error-source" data-lang="mermaid">
          <code>{source}</code>
        </pre>
        <details className="mermaid-diagram__error-details">
          <summary>Error details</summary>
          <pre className="mermaid-diagram__error-detail-text">{state.message}</pre>
        </details>
      </div>
    );
  }

  return <SvgPreview refEl={previewRef} svg={state.svg} />;
}

function SvgPreview({ refEl, svg }: { refEl: RefObject<HTMLDivElement | null>; svg: string }) {
  const openAnchor = useCallback((event: React.MouseEvent) => {
    const target = event.target;
    if (!(target instanceof Element)) return;
    const anchor = target.closest("a");
    const href = anchor ? getMermaidAnchorHref(anchor) : null;
    if (!href) return;
    event.preventDefault();
    if (isOpenableMermaidHref(href)) openExternal(href);
  }, []);

  const preventMiddleButtonNavigation = useCallback((event: React.MouseEvent) => {
    if (event.button !== 1) return;
    const target = event.target;
    if (target instanceof Element && target.closest("a")) event.preventDefault();
  }, []);

  return (
    <div className="mermaid-diagram__preview-wrap">
      <div
        className="mermaid-diagram__preview"
        ref={refEl}
        dangerouslySetInnerHTML={{ __html: svg }}
        onClick={openAnchor}
        onAuxClick={openAnchor}
        onMouseDown={preventMiddleButtonNavigation}
      />
    </div>
  );
}

export default MermaidDiagram;
