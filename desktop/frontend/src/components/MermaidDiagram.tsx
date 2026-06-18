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

/**
 * Queue a mermaid.render() call behind all previous ones to avoid corruption
 * from concurrent renders on Mermaid's shared global state. Accepts an
 * AbortSignal so obsolete renders (unmounted or superseded by streaming) can
 * be skipped without burning CPU in the queue.
 */
function queuedRender(
  svgId: string,
  definition: string,
  theme: "dark" | "default",
  signal: AbortSignal,
): Promise<{ svg: string; bindFunctions: (element: Element) => void }> {
  const promise = renderQueue.then(async () => {
    if (signal.aborted) throw new DOMException("Aborted", "AbortError");
    mermaidModule!.default.initialize({
      startOnLoad: false,
      theme,
      maxTextSize: 100000,
      securityLevel: "antiscript",
      fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
    });
    const result = await mermaidModule!.default.render(svgId, definition);
    if (signal.aborted) throw new DOMException("Aborted", "AbortError");
    return result as { svg: string; bindFunctions: (element: Element) => void };
  });
  // Chain the error handler separately so one failure doesn't kill the queue.
  renderQueue = promise.then(() => {}, () => {});
  return promise;
}

// ── SVG sanitisation ─────────────────────────────────────────────────────

/**
 * Sanitise a rendered Mermaid SVG string before injecting it into the DOM.
 * Keeps `<a>` elements (we handle link security ourselves via openExternal)
 * but removes:
 *   - `<script>` elements
 *   - event-handler attributes (onclick, onload, etc.)
 *   - javascript: and data: URLs in href / xlink:href
 */
function sanitizeSvg(svg: string): string {
  if (typeof DOMParser === "undefined") return svg;
  const doc = new DOMParser().parseFromString(svg, "image/svg+xml");
  const svgEl = doc.documentElement;
  if (svgEl.tagName !== "svg") return svg;

  const walker = doc.createTreeWalker(svgEl, NodeFilter.SHOW_ELEMENT, null);
  const toRemove: Element[] = [];
  let node: Element | null;
  while ((node = walker.nextNode() as Element | null)) {
    // Remove <script> elements entirely
    if (node.tagName === "script") {
      toRemove.push(node);
      continue;
    }
    // Strip event-handler attributes (onclick, onload, etc.)
    // and sanitise javascript:/data: URLs in href-like attributes.
    for (const attr of Array.from(node.attributes)) {
      if (/^on/i.test(attr.name)) {
        node.removeAttribute(attr.name);
        continue;
      }
      if (attr.name === "href" || attr.name === "xlink:href") {
        const val = attr.value.trim().toLowerCase();
        if (val.startsWith("javascript:") || val.startsWith("data:")) {
          node.removeAttribute(attr.name);
        }
      }
    }
  }
  for (const el of toRemove) {
    el.parentNode?.removeChild(el);
  }
  return new XMLSerializer().serializeToString(svgEl);
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
  const [bindFn, setBindFn] = useState<((el: Element) => void) | null>(null);
  const theme = useMermaidTheme();
  const instanceId = useId().replace(/[:.]/g, "-");
  const svgId = `mermaid-${instanceId}`;
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;
    const controller = new AbortController();
    setBindFn(null);

    (async () => {
      try {
        if (!definition.trim()) {
          setState({ status: "error", message: "Empty diagram definition" });
          return;
        }

        await ensureMermaid();
        if (controller.signal.aborted || !mountedRef.current) return;

        const result = await queuedRender(svgId, definition, theme, controller.signal);
        if (controller.signal.aborted || !mountedRef.current) return;

        const svg = sanitizeSvg(result.svg);
        setState({ status: "rendered", svg });
        if (result.bindFunctions) {
          setBindFn(() => result.bindFunctions);
        }
      } catch (err) {
        // AbortError from our own AbortSignal is expected on unmount/update
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
        <pre className="code hljs mermaid-diagram__error-source" data-lang="mermaid">
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
      <SvgWrapper svg={state.svg} bindFunctions={bindFn} />
      <CopyButton text={definition} className="code-block__copy" />
    </div>
  );
});

/**
 * Renders the SVG string returned by mermaid into the DOM via
 * dangerouslySetInnerHTML. Calls bindFunctions after the SVG is in the
 * DOM so Mermaid click interactions (click directives, tooltips) work.
 * Intercepts clicks on SVG `<a>` elements and routes them through
 * openExternal to avoid in-app navigation.
 */
function SvgWrapper({
  svg,
  bindFunctions,
}: {
  svg: string;
  bindFunctions: ((el: Element) => void) | null;
}) {
  const ref = useRef<HTMLDivElement>(null);

  // Call bindFunctions after the SVG content is in the DOM so that Mermaid
  // click interactions (click directives, tooltips) are wired up.
  useEffect(() => {
    if (bindFunctions && ref.current) {
      bindFunctions(ref.current);
    }
  }, [bindFunctions]);

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
      ref={ref}
      className="mermaid-diagram__svg"
      dangerouslySetInnerHTML={{ __html: svg }}
      onClick={handleClick}
      onAuxClick={handleAuxClick}
      onMouseDown={handleMouseDown}
    />
  );
}
