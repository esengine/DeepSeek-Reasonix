import { memo, useCallback, useEffect, useId, useRef, useState } from "react";
import { AlertCircle } from "lucide-react";
import { CopyButton } from "./CopyButton";
import { openExternal } from "../lib/bridge";

// MermaidDiagram renders a mermaid fenced code block as an interactive SVG
// diagram using the mermaid.js renderer. The theme (dark/light) follows the
// app's current data-theme attribute.

interface MermaidDiagramProps {
  definition: string;
}

type DiagramState =
  | { status: "loading" }
  | { status: "rendered"; svg: string }
  | { status: "error"; message: string };

// ── Module-level lazy import + render serialisation ──────────────────────
// Mermaid's renderer uses shared global state; concurrent render() calls
// from multiple instances can corrupt each other's output, so we serialise
// every render call through a module-level promise chain.

let mermaidModule: typeof import("mermaid") | null = null;
let initPromise: Promise<void> | null = null;
let renderQueue: Promise<void> = Promise.resolve();

async function ensureMermaid(): Promise<void> {
  if (mermaidModule) return;
  if (initPromise) return initPromise;
  initPromise = import("mermaid").then((mod) => {
    mermaidModule = mod;
    mermaidModule.default.initialize({
      startOnLoad: false,
      theme: "dark",
      maxTextSize: 100000,
      securityLevel: "antiscript",
      fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
    });
  }).catch((err) => {
    // Reset so a later render can retry on transient failures.
    initPromise = null;
    mermaidModule = null;
    throw err;
  });
  return initPromise;
}

function queuedRender(svgId: string, definition: string, theme: "dark" | "default"): Promise<string> {
  const promise = renderQueue.then(async () => {
    mermaidModule!.default.initialize({
      startOnLoad: false,
      theme,
      maxTextSize: 100000,
      securityLevel: "antiscript",
      fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
    });
    const { svg } = await mermaidModule!.default.render(svgId, definition);
    return svg;
  });
  // Chain the error handler separately so one failure doesn't kill the queue.
  renderQueue = promise.then(() => {}, () => {});
  return promise;
}

// ── Theme detection ──────────────────────────────────────────────────────

function resolveMermaidTheme(): "dark" | "default" {
  if (typeof document === "undefined") return "dark";
  const html = document.documentElement;
  const forced = html.getAttribute("data-theme");
  if (forced === "light") return "default";
  if (forced === "dark") return "dark";
  // auto / no attribute → follow OS
  return window.matchMedia("(prefers-color-scheme: light)").matches ? "default" : "dark";
}

function useMermaidTheme(): "dark" | "default" {
  const [theme, setTheme] = useState<"dark" | "default">(resolveMermaidTheme);

  useEffect(() => {
    // Observe data-theme changes on <html> so the diagram re-renders when
    // the user switches between light/dark mode.
    const html = document.documentElement;
    const observer = new MutationObserver(() => {
      setTheme(resolveMermaidTheme());
    });
    observer.observe(html, { attributeFilter: ["data-theme", "data-theme-mode"] });
    // Also listen for OS-level changes when in auto mode.
    const mq = window.matchMedia("(prefers-color-scheme: light)");
    const onOsChange = () => setTheme(resolveMermaidTheme());
    mq.addEventListener("change", onOsChange);
    return () => {
      observer.disconnect();
      mq.removeEventListener("change", onOsChange);
    };
  }, []);

  return theme;
}

// ── Component ────────────────────────────────────────────────────────────

export const MermaidDiagram = memo(function MermaidDiagram({ definition }: MermaidDiagramProps) {
  const [state, setState] = useState<DiagramState>({ status: "loading" });
  const theme = useMermaidTheme();
  const instanceId = useId().replace(/[:.]/g, "-");
  const svgId = `mermaid-${instanceId}`;
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;
    let cancelled = false;

    (async () => {
      try {
        if (!definition.trim()) {
          setState({ status: "error", message: "Empty diagram definition" });
          return;
        }

        await ensureMermaid();
        if (cancelled || !mountedRef.current) return;

        const svg = await queuedRender(svgId, definition, theme);
        if (cancelled || !mountedRef.current) return;

        setState({ status: "rendered", svg });
      } catch (err) {
        if (cancelled || !mountedRef.current) return;
        const message = err instanceof Error ? err.message : String(err);
        setState({ status: "error", message });
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [definition, svgId, theme]);

  if (state.status === "loading") {
    return (
      <div className="mermaid-diagram mermaid-diagram--loading">
        <div className="mermaid-diagram__placeholder">
          <span className="mermaid-diagram__spinner" />
          <span>Rendering diagram…</span>
        </div>
      </div>
    );
  }

  if (state.status === "error") {
    return (
      <div className="mermaid-diagram mermaid-diagram--error">
        <div className="mermaid-diagram__error-bar">
          <AlertCircle size={14} className="mermaid-diagram__error-icon" />
          <span>Diagram syntax error</span>
        </div>
        <pre className="code hljs" data-lang="mermaid">
          <code>{definition}</code>
        </pre>
        <details className="mermaid-diagram__error-details">
          <summary>Error details</summary>
          <pre className="mermaid-diagram__error-detail-text">{state.message}</pre>
        </details>
        <CopyButton text={definition} className="code-block__copy" />
      </div>
    );
  }

  return (
    <div className="mermaid-diagram mermaid-diagram--rendered">
      <SvgWrapper svg={state.svg} />
      <CopyButton text={definition} className="code-block__copy" />
    </div>
  );
});

/**
 * Renders the SVG string returned by mermaid into the DOM via
 * dangerouslySetInnerHTML. Intercepts clicks on SVG `<a>` elements
 * and routes them through openExternal to avoid in-app navigation.
 */
function SvgWrapper({ svg }: { svg: string }) {
  const handleClick = useCallback((e: React.MouseEvent) => {
    const anchor = (e.target as Element).closest("a");
    if (!anchor || !anchor.getAttribute("href")) return;
    e.preventDefault();
    const href = anchor.getAttribute("href")!;
    openExternal(href);
  }, []);

  const handleAuxClick = useCallback((e: React.MouseEvent) => {
    const anchor = (e.target as Element).closest("a");
    if (!anchor) return;
    e.preventDefault();
    if (anchor.getAttribute("href")) {
      openExternal(anchor.getAttribute("href")!);
    }
  }, []);

  const handleMouseDown = useCallback((e: React.MouseEvent) => {
    if (e.button === 1) {
      const anchor = (e.target as Element).closest("a");
      if (anchor) e.preventDefault();
    }
  }, []);

  return (
    <div
      className="mermaid-diagram__svg"
      dangerouslySetInnerHTML={{ __html: svg }}
      onClick={handleClick}
      onAuxClick={handleAuxClick}
      onMouseDown={handleMouseDown}
    />
  );
}
