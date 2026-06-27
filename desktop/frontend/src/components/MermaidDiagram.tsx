import { useCallback, useEffect, useId, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import mermaid from "mermaid";
import svgPanZoom from "svg-pan-zoom";
import { Presentation, Code2, Play, Maximize2, Minimize2 } from "lucide-react";
import { CopyButton } from "./CopyButton";

// ── MERMAID DIAGRAM ─────────────────────────────────────────────────────────
// Renders ```mermaid fenced code blocks with a toolbar header:
//   [mermaid]   Code | Preview   [⛶ Fullscreen]
//
// - Preview: rendered SVG via mermaid + svg-pan-zoom (SVG CTM, always sharp)
// - Code: raw mermaid source in a <pre> block
// - Fullscreen: expands the diagram to fill the chat-pane via portal
//
// Zoom/pan uses industry-standard SVG CTM manipulation (svg-pan-zoom), the same
// approach used by the Mermaid Live Editor. No CSS zoom or transform hacks.

// ── Mermaid initialisation ──────────────────────────────────────────────────

let mermaidInitialised = false;

function ensureMermaidInit() {
  if (mermaidInitialised) return;
  mermaidInitialised = true;
  mermaid.initialize({
    startOnLoad: false,
    theme: "base",
    securityLevel: "strict",
  });
}

function resolveTheme(): "dark" | "light" {
  const attr = document.documentElement.getAttribute("data-theme");
  if (attr === "light" || attr === "dark") return attr;
  return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

function mermaidThemeVars(theme: "dark" | "light"): Record<string, string | number | boolean> {
  if (theme === "dark") {
    return {
      darkMode: true, background: "#111319", fontSize: "13px",
      fontFamily: "system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif",
      primaryColor: "#1f2937", primaryTextColor: "#f4f5f7", primaryBorderColor: "#374151",
      mainBkg: "#1f2937", nodeBkg: "#1f2937", nodeBorder: "#374151", nodeTextColor: "#f4f5f7",
      stateBkg: "#1f2937", stateBorder: "#374151", stateLabelColor: "#f4f5f7",
      labelColor: "#9ca3af", lineColor: "#6b7280", textColor: "#9ca3af",
      defaultLinkColor: "#6b7280", edgeLabelBackground: "#111319",
      clusterBkg: "#0f172a", clusterBorder: "#1f2937",
      actorBkg: "#1f2937", actorBorder: "#374151", actorTextColor: "#f4f5f7",
      actorLineColor: "#4b5563", signalColor: "#9ca3af", signalTextColor: "#f4f5f7",
      labelBoxBkgColor: "#1f2937", labelBoxBorderColor: "#374151", labelTextColor: "#f4f5f7",
      loopTextColor: "#f4f5f7", noteBkgColor: "#0f172a", noteBorderColor: "#374151",
      noteTextColor: "#e5e7eb", classText: "#f4f5f7", classBorder: "#374151",
      classBkg: "#1f2937", secondaryColor: "#111319", tertiaryColor: "#1f2937",
    };
  }
  return {
    darkMode: false, background: "#ffffff", fontSize: "13px",
    fontFamily: "system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif",
    primaryColor: "#f8fafc", primaryTextColor: "#0f172a", primaryBorderColor: "#cbd5e1",
    mainBkg: "#f8fafc", nodeBkg: "#f8fafc", nodeBorder: "#cbd5e1", nodeTextColor: "#0f172a",
    stateBkg: "#f8fafc", stateBorder: "#cbd5e1", stateLabelColor: "#0f172a",
    labelColor: "#475569", lineColor: "#94a3b8", textColor: "#475569",
    defaultLinkColor: "#94a3b8", edgeLabelBackground: "#ffffff",
    clusterBkg: "#f1f5f9", clusterBorder: "#e2e8f0",
    actorBkg: "#f8fafc", actorBorder: "#cbd5e1", actorTextColor: "#0f172a",
    actorLineColor: "#cbd5e1", signalColor: "#475569", signalTextColor: "#0f172a",
    labelBoxBkgColor: "#f8fafc", labelBoxBorderColor: "#cbd5e1", labelTextColor: "#0f172a",
    loopTextColor: "#0f172a", noteBkgColor: "#f1f5f9", noteBorderColor: "#e2e8f0",
    noteTextColor: "#334155", classText: "#0f172a", classBorder: "#cbd5e1",
    classBkg: "#f8fafc", secondaryColor: "#f1f5f9", tertiaryColor: "#f8fafc",
  };
}

// ── Constants ───────────────────────────────────────────────────────────────

type Tab = "code" | "preview";

const MIN_ZOOM = 0.3;
const MAX_ZOOM = 8;

function destroyPanZoom(instance: ReturnType<typeof svgPanZoom>) {
  try {
    instance.destroy();
  } catch (e) {
    console.warn("Failed to clean up Mermaid pan/zoom instance", e);
  }
}

interface MermaidDiagramProps {
  text: string;
}

// ── Component ───────────────────────────────────────────────────────────────

export default function MermaidDiagram({ text }: MermaidDiagramProps) {
  const previewRef = useRef<HTMLDivElement>(null);
  const svgContainerRef = useRef<HTMLDivElement>(null);
  const panZoomRef = useRef<ReturnType<typeof svgPanZoom> | null>(null);
  const id = useId().replace(/[^a-zA-Z0-9_-]/g, "_");
  const [error, setError] = useState<string | null>(null);
  const [svg, setSvg] = useState<string | null>(null);
  const [tab, setTab] = useState<Tab>("preview");
  const [fullscreen, setFullscreen] = useState(false);
  const [themeVersion, setThemeVersion] = useState(0);
  const [fullscreenContainer, setFullscreenContainer] = useState<Element | null>(null);

  // ── Render SVG ──────────────────────────────────────────────────────────

  useEffect(() => {
    let cancelled = false;
    ensureMermaidInit();

    (async () => {
      try {
        const theme = resolveTheme();
        mermaid.initialize({
          startOnLoad: false,
          theme: "base",
          securityLevel: "strict",
          themeVariables: mermaidThemeVars(theme),
        });
        const { svg: renderedSvg } = await mermaid.render(
          `mermaid-${id}`,
          text.trim(),
        );
        if (!cancelled) {
          setSvg(renderedSvg);
          setError(null);
        }
      } catch (e: unknown) {
        if (!cancelled) {
          setError(e instanceof Error ? e.message : String(e));
          setSvg(null);
        }
      }
    })();

    return () => { cancelled = true; };
  }, [text, id, themeVersion]);

  // ── svg-pan-zoom: attach after SVG is in DOM ────────────────────────────

  useLayoutEffect(() => {
    if (tab !== "preview" || !svg) return;
    const container = svgContainerRef.current;
    if (!container) return;

    let initFrame = 0;
    let fitFrame = 0;
    let svgEl: SVGSVGElement | null = null;
    let instance: ReturnType<typeof svgPanZoom> | null = null;

    const cleanup = () => {
      if (initFrame) {
        cancelAnimationFrame(initFrame);
        initFrame = 0;
      }
      if (fitFrame) {
        cancelAnimationFrame(fitFrame);
        fitFrame = 0;
      }
      if (panZoomRef.current === instance) {
        panZoomRef.current = null;
      }
      if (instance) {
        destroyPanZoom(instance);
        instance = null;
      }
      svgEl = null;
    };

    // Wait for React to commit the SVG before svg-pan-zoom wraps it.
    initFrame = requestAnimationFrame(() => {
      initFrame = 0;
      svgEl = container.querySelector("svg") as SVGSVGElement | null;
      if (!svgEl) return;
      svgEl.removeAttribute("width");
      svgEl.removeAttribute("height");
      svgEl.style.width = "100%";
      svgEl.style.height = "100%";
      svgEl.style.maxWidth = "none";

      // Clean up previous instance
      if (panZoomRef.current) {
        destroyPanZoom(panZoomRef.current);
        panZoomRef.current = null;
      }

      const pz = svgPanZoom(svgEl, {
        zoomEnabled: true,
        panEnabled: true,
        controlIconsEnabled: false,
        dblClickZoomEnabled: true,
        fit: true,
        center: true,
        minZoom: MIN_ZOOM,
        maxZoom: MAX_ZOOM,
        zoomScaleSensitivity: 0.3,
        beforePan: function (_oldPan, newPan) {
          const current = panZoomRef.current;
          if (!current) return newPan;
          // Clamp pan to prevent dragging the SVG completely out of view
          const zoom = current.getZoom();
          const sizes = current.getSizes();
          const maxX = Math.max(0, (sizes.width * zoom - sizes.width) / zoom / 2 + sizes.width * 0.2);
          const maxY = Math.max(0, (sizes.height * zoom - sizes.height) / zoom / 2 + sizes.height * 0.2);
          return {
            x: Math.max(-maxX, Math.min(maxX, newPan.x)),
            y: Math.max(-maxY, Math.min(maxY, newPan.y)),
          };
        },
      });

      instance = pz;
      panZoomRef.current = pz;

      fitFrame = requestAnimationFrame(() => {
        fitFrame = 0;
        if (panZoomRef.current !== pz) return;
        pz.resize();
        pz.fit();
        pz.center();
      });
    });

    return cleanup;
  }, [svg, tab, fullscreen]);

  // ── Theme change listeners ──────────────────────────────────────────────

  useEffect(() => {
    const mq = window.matchMedia("(prefers-color-scheme: dark)");
    const onChange = () => { setSvg(null); setError(null); setThemeVersion((v) => v + 1); };
    mq.addEventListener("change", onChange);
    return () => mq.removeEventListener("change", onChange);
  }, []);

  useEffect(() => {
    const observer = new MutationObserver(() => { setSvg(null); setError(null); setThemeVersion((v) => v + 1); });
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["data-theme"],
    });
    return () => observer.disconnect();
  }, []);

  // ── ResizeObserver: keep svg-pan-zoom in sync ───────────────────────────

  useEffect(() => {
    const container = svgContainerRef.current;
    if (!container || tab !== "preview" || !svg) return;
    const ro = new ResizeObserver(() => {
      if (panZoomRef.current) panZoomRef.current.resize();
    });
    ro.observe(container);
    return () => ro.disconnect();
  }, [svg, tab, fullscreen]);

  // ── Fullscreen ───────────────────────────────────────────────────────────

  useEffect(() => {
    if (!fullscreen) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setFullscreen(false);
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [fullscreen]);

  const toggleFullscreen = useCallback(() => {
    setFullscreen((v) => {
      if (!v) setFullscreenContainer(document.querySelector(".chat-pane"));
      else setFullscreenContainer(null);
      return !v;
    });
  }, []);

  // Auto-fit after portal mounts in fullscreen
  useEffect(() => {
    if (!fullscreen || !fullscreenContainer) return;
    const timer = setTimeout(() => {
      if (panZoomRef.current) {
        panZoomRef.current.resize();
        panZoomRef.current.fit();
        panZoomRef.current.center();
      }
    }, 100);
    return () => clearTimeout(timer);
  }, [fullscreen, fullscreenContainer]);

  // ── Zoom controls (for +/- buttons) ─────────────────────────────────────

  const zoomIn = useCallback(() => {
    const pz = panZoomRef.current;
    if (pz) pz.zoomIn();
  }, []);
  const zoomOut = useCallback(() => {
    const pz = panZoomRef.current;
    if (pz) pz.zoomOut();
  }, []);

  // ── Render ──────────────────────────────────────────────────────────────

  const toolbar = (
    <div className="mermaid-diagram__toolbar">
      <div className="mermaid-diagram__title">
        <Presentation className="mermaid-diagram__title-icon" size={14} />
        <span className="mermaid-diagram__title-text">Mermaid</span>
      </div>
      <div className="mermaid-diagram__actions">
        <button
          className={`mermaid-diagram__icon-btn${tab === "code" ? " mermaid-diagram__icon-btn--active" : ""}`}
          onClick={() => setTab("code")}
          title="Code Source"
        ><Code2 size={15} /></button>
        <button
          className={`mermaid-diagram__icon-btn${tab === "preview" ? " mermaid-diagram__icon-btn--active" : ""}`}
          onClick={() => setTab("preview")}
          title="Preview Diagram"
        ><Play size={15} /></button>
        <CopyButton text={text} className="mermaid-diagram__copy-btn" showInlineLabel={false} />
        <button
          className="mermaid-diagram__icon-btn"
          onClick={toggleFullscreen}
          title={fullscreen ? "Exit fullscreen" : "Fullscreen"}
        >{fullscreen ? <Minimize2 size={14} /> : <Maximize2 size={14} />}</button>
      </div>
    </div>
  );

  const body = tab === "code" ? (
    <pre className="mermaid-diagram__code"><code>{text}</code></pre>
  ) : error ? (
    <div className="mermaid-diagram__error-msg">
      <span className="mermaid-diagram__error-label">Mermaid render error</span>
      <pre className="mermaid-diagram__source">{text}</pre>
    </div>
  ) : svg ? (
    <div className="mermaid-diagram__preview-wrapper">
      <div className="mermaid-diagram__preview" ref={svgContainerRef}
        dangerouslySetInnerHTML={{ __html: svg }}
      />
      <div className="mermaid-diagram__zoom">
        <button className="mermaid-diagram__zoom-btn" onClick={zoomOut} title="Zoom out">−</button>
        <button className="mermaid-diagram__zoom-btn" onClick={zoomIn} title="Zoom in">+</button>
      </div>
    </div>
  ) : (
    <div className="mermaid-diagram__loading">Rendering diagram…</div>
  );

  const rootCls = ["mermaid-diagram", error && "mermaid-diagram--error"].filter(Boolean).join(" ");

  if (fullscreen && fullscreenContainer) {
    return (
      <>
        <div className={rootCls} ref={previewRef}>
          {toolbar}
          <div className="mermaid-diagram__fullscreen-placeholder">
            Diagram in fullscreen — press Esc or click Exit to return
          </div>
        </div>
        {createPortal(
          <div className="mermaid-diagram--fullscreen">
            <div className="mermaid-diagram--fullscreen__inner">
              {toolbar}
              <div className="mermaid-diagram--fullscreen__body">{body}</div>
            </div>
          </div>,
          fullscreenContainer,
        )}
      </>
    );
  }

  return (
    <div className={rootCls} ref={previewRef}>
      {toolbar}
      {body}
    </div>
  );
}
