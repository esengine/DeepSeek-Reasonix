import { memo, useEffect, useRef, useState } from "react";
import { openExternal } from "../lib/bridge";

// Maps standard CSS variable names to host variable names.
// WKWebView's CSSStyleDeclaration doesn't enumerate custom
// properties via cs.length/cs[i], so we must use explicit
// getPropertyValue() calls.
const HOST_VAR_MAP: Record<string, string[]> = {
  "--background": ["--bg"],
  "--foreground": ["--fg"],
  "--primary": ["--accent"],
  "--primary-foreground": ["--accent-fg"],
  "--muted": ["--bg-soft"],
  "--muted-foreground": ["--fg-dim"],
  "--card": ["--bg-elev"],
  "--accent": ["--accent"],
  "--accent-fg": ["--accent-fg"],
  "--accent-foreground": ["--accent-fg"],
  "--bg": ["--bg"],
  "--fg": ["--fg"],
  "--bg-soft": ["--bg-soft"],
  "--fg-dim": ["--fg-dim"],
  "--bg-elev": ["--bg-elev"],
  "--border": ["--border"],
  "--radius": ["--radius"],
  "--font-sans": ["--font-sans"],
  "--font-mono": ["--font-mono"],
};

// Reads the host theme variables into a plain map. The widget renders in an
// opaque-origin sandbox (see below), so the host can no longer reach into the
// iframe's contentDocument to set variables imperatively — theme travels in via
// srcdoc for the first paint and via postMessage for later switches.
function collectThemeVars(): Record<string, string> {
  const out: Record<string, string> = {};
  if (typeof document === "undefined") return out;
  const cs = getComputedStyle(document.documentElement);
  for (const [std, hosts] of Object.entries(HOST_VAR_MAP)) {
    for (const host of hosts) {
      const val = cs.getPropertyValue(host).trim();
      if (val) {
        out[std] = val;
        break;
      }
    }
  }
  return out;
}

function varsToCss(vars: Record<string, string>): string {
  const decls = Object.entries(vars).map(([k, v]) => `${k}:${v}`);
  return `:root{${decls.join(";")}}`;
}

// Content-Security-Policy for the widget document.
//
// Security notes:
//   - default-src 'none' + an explicit allowlist so a prompt-injected widget
//     cannot reach arbitrary origins.
//   - img-src is restricted to `data:` (was `https:`, which let a widget beacon
//     data out to any host via <img> even with connect-src 'none').
//   - object-src/base-uri/frame-src/child-src are locked to 'none'.
//   - script-src keeps the CDN allowlist for Chart.js/mermaid + 'unsafe-inline'
//     for the widget's own inline scripts and 'unsafe-eval' for libraries that
//     need it.
const CDN_HOSTS = "cdnjs.cloudflare.com esm.sh cdn.jsdelivr.net unpkg.com";
const WIDGET_CSP = [
  "default-src 'none'",
  `script-src 'unsafe-inline' 'unsafe-eval' ${CDN_HOSTS}`,
  `style-src 'unsafe-inline' ${CDN_HOSTS}`,
  "img-src data:",
  `font-src data: ${CDN_HOSTS}`,
  "connect-src 'none'",
  "form-action 'none'",
  "base-uri 'none'",
  "object-src 'none'",
  "frame-src 'none'",
  "child-src 'none'",
].join("; ");

const SHELL_CSS = `
*{box-sizing:border-box;margin:0;padding:0}
html,body{overflow:hidden;background:transparent}
body{font-family:var(--font-sans);color:var(--foreground);background:transparent;line-height:1.5;padding:0}
.card{background:var(--card);border:0.5px solid var(--border);border-radius:calc(var(--radius)*1.5);padding:1rem 1.25rem}
button{font-size:12px;padding:0 14px;height:32px;border-radius:var(--radius);border:0.5px solid var(--border);background:var(--card);color:var(--foreground);cursor:pointer;font-family:var(--font-sans);line-height:1;display:inline-flex;align-items:center;gap:6px}
button.primary{background:var(--primary);color:var(--primary-foreground);border-color:var(--primary)}
button.destructive{background:color-mix(in srgb,var(--destructive, #E24B4A) 10%,transparent);color:var(--destructive, #E24B4A);border-color:color-mix(in srgb,var(--destructive, #E24B4A) 30%,transparent)}
input,textarea,select{font-size:12px;padding:4px 8px;border-radius:var(--radius);border:0.5px solid var(--border);background:var(--card);color:var(--foreground);font-family:var(--font-sans)}
code{font-family:var(--font-mono);font-size:11px;background:var(--muted);padding:1px 4px;border-radius:3px}
pre{font-family:var(--font-mono);font-size:11px;background:var(--muted);padding:8px;border-radius:var(--radius);overflow-x:auto}
.metric{background:var(--muted);border-radius:var(--radius);padding:8px 12px;text-align:center}
.metric-value{font-size:20px;font-weight:600;letter-spacing:-.02em;line-height:1.2}
.metric-label{font-size:11px;color:var(--muted-foreground);margin-top:2px}
.label{font-size:11px;color:var(--muted-foreground)}
.row{display:flex;gap:12px;align-items:center}
.row.wrap{flex-wrap:wrap}
.col{display:flex;flex-direction:column;gap:8px}
.grid-2{display:grid;grid-template-columns:1fr 1fr;gap:8px}
.grid-3{display:grid;grid-template-columns:1fr 1fr 1fr;gap:8px}
.badge{display:inline-flex;align-items:center;font-size:11px;padding:0 8px;height:18px;border-radius:999px;background:var(--muted);color:var(--foreground);white-space:nowrap}
.badge.primary{background:color-mix(in srgb,var(--accent) 15%,transparent);color:var(--accent)}
.t{font-family:var(--font-sans);font-size:14px;fill:var(--fg)}
.ts{font-family:var(--font-sans);font-size:12px;fill:var(--fg-dim)}
.th{font-family:var(--font-sans);font-size:14px;font-weight:500;fill:var(--fg)}
.box{fill:var(--bg-elev-2,var(--bg-soft));stroke:var(--border);stroke-width:0.5}
.node{cursor:pointer;transition:opacity .15s}
.node:hover{opacity:.7}
.arr{stroke:var(--fg-dim);stroke-width:1.5;fill:none;stroke-linecap:round}
.leader{stroke:var(--fg-faint,var(--fg-dim));stroke-width:0.5;stroke-dasharray:4 3;fill:none}
.c-blue{fill:#3B82F6;stroke:#2563EB;color:#fff}
.c-teal{fill:#14B8A6;stroke:#0D9488;color:#fff}
.c-amber{fill:#F59E0B;stroke:#D97706;color:#fff}
.c-green{fill:#22C55E;stroke:#16A34A;color:#fff}
.c-red{fill:#EF4444;stroke:#DC2626;color:#fff}
.c-purple{fill:#8B5CF6;stroke:#7C3AED;color:#fff}
.c-coral{fill:#FB7185;stroke:#F43F5E;color:#fff}
.c-pink{fill:#EC4899;stroke:#DB2777;color:#fff}
.c-gray{fill:#6B7280;stroke:#4B5563;color:#fff}
.c-blue,.c-teal,.c-amber,.c-green,.c-red,.c-purple,.c-coral,.c-pink,.c-gray{stroke-width:0.5}
.c-blue>.t,.c-blue>.ts,.c-blue>.th,.c-teal>.t,.c-teal>.ts,.c-teal>.th,.c-amber>.t,.c-amber>.ts,.c-amber>.th,.c-green>.t,.c-green>.ts,.c-green>.th,.c-red>.t,.c-red>.ts,.c-red>.th,.c-purple>.t,.c-purple>.ts,.c-purple>.th,.c-coral>.t,.c-coral>.ts,.c-coral>.th,.c-pink>.t,.c-pink>.ts,.c-pink>.th,.c-gray>.t,.c-gray>.ts,.c-gray>.th{fill:#fff}
`;

// Bootstrap runs inside the sandboxed widget document. It never has access to
// the host window (opaque origin) — its only channel to the host is
// postMessage. It reports height, exposes sendPrompt/openLink, intercepts
// anchor clicks, and applies theme-variable updates pushed by the host.
//
// sendPrompt/openLink only *request* an action; the host requires a real click
// on host-owned confirmation UI before sending a message or opening a link.
// This is why the payload carries no "trusted" flag: a widget script can post
// any message it likes, so the host must not treat a self-reported gesture as
// proof. The confirmation gesture happens outside the iframe, where the widget
// cannot forge it.
const BOOTSTRAP = `<script>
(function(){
  var h=-1,raf=null;
  function report(){var s=document.documentElement.scrollHeight;if(s===h)return;h=s;parent.postMessage({type:"ws",height:s},"*");}
  function q(){if(!raf)raf=requestAnimationFrame(function(){raf=null;report();});}
  if(window.ResizeObserver)new ResizeObserver(q).observe(document.body);
  window.addEventListener("load",q);
  window.sendPrompt=function(x){parent.postMessage({type:"wp",text:String(x)},"*");};
  window.openLink=function(u){parent.postMessage({type:"wl",url:String(u)},"*");};
  document.addEventListener("click",function(e){
    var t=e.target,a=t&&t.closest?t.closest("a[href]"):null;
    if(!a)return;var href=a.getAttribute("href")||"";
    if(/^(https?:|mailto:)/i.test(href)){e.preventDefault();parent.postMessage({type:"wl",url:href},"*");}
  },true);
  window.addEventListener("message",function(e){
    if(e.source!==parent)return;
    var d=e.data;
    if(d&&d.type==="theme"&&d.vars){
      var r=document.documentElement;
      for(var k in d.vars){if(k.indexOf("--")===0)r.style.setProperty(k,String(d.vars[k]));}
      q();
    }
  });
})();
</script>`;

// Builds the full sandboxed document for the current widget source + theme.
// Exported for contract tests.
export function buildWidgetDoc(body: string, themeCss: string): string {
  return `<!DOCTYPE html><html><head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta http-equiv="Content-Security-Policy" content="${WIDGET_CSP}">
<style>
${themeCss}
${SHELL_CSS}
</style>
</head>
<body>
${body}
${BOOTSTRAP}
</body></html>`;
}

// A widget-initiated action awaiting host confirmation. `value` is the prompt
// text or the link URL.
export type WidgetRequest = { id: number; kind: "prompt" | "link"; value: string };

let nextRequestId = 1;

export type WidgetMessageHandlers = {
  setHeight: (px: number) => void;
  requestPrompt: (text: string) => void;
  requestLink: (url: string) => void;
};

// Routes a postMessage payload from the widget to the host. Pure and
// side-effect-free apart from the handler callbacks, so the gates are
// unit-testable. The caller verifies the source (e.source === contentWindow)
// before calling. Note this only *requests* prompt/link actions — it never
// sends or opens directly, because the payload cannot be trusted to reflect a
// real user gesture. Actual dispatch requires host confirmation.
export function routeWidgetMessage(data: unknown, h: WidgetMessageHandlers): void {
  if (!data || typeof data !== "object") return;
  const d = data as { type?: unknown; height?: unknown; text?: unknown; url?: unknown };
  if (d.type === "ws" && typeof d.height === "number") {
    h.setHeight(Math.min(Math.max(d.height, 50), 2000));
    return;
  }
  if (d.type === "wp" && typeof d.text === "string") {
    h.requestPrompt(d.text);
    return;
  }
  if (d.type === "wl" && typeof d.url === "string") {
    // F4: only http(s)/mailto can even be offered; block javascript:, file:, etc.
    if (/^(https?:|mailto:)/i.test(d.url)) h.requestLink(d.url);
  }
}

const RENDER_DEBOUNCE_MS = 100;
const RENDER_MAX_WAIT_MS = 500;

function postTheme(iframe: HTMLIFrameElement | null) {
  iframe?.contentWindow?.postMessage({ type: "theme", vars: collectThemeVars() }, "*");
}

export const StreamWidget = memo(function StreamWidget({ value }: { value: string }) {
  const iframeRef = useRef<HTMLIFrameElement>(null);
  const valueRef = useRef(value);
  valueRef.current = value;
  const renderTimer = useRef(0);
  const lastRenderAt = useRef(0);
  // A widget-initiated action (send prompt / open link) awaiting a real click
  // on the host-owned confirmation strip below the iframe.
  const [pending, setPending] = useState<WidgetRequest | null>(null);
  const [expanded, setExpanded] = useState(false);

  function renderNow() {
    const iframe = iframeRef.current;
    if (!iframe) return;
    lastRenderAt.current = Date.now();
    // Clear any pending request — the iframe content was just reloaded so
    // the request's origin context is gone.
    setPending(null);
    // Opaque-origin sandbox: the widget cannot script the host. Content is
    // handed in via srcdoc rather than the host writing contentDocument, which
    // is what previously required allow-same-origin. Theme is inlined for the
    // first paint; later theme switches are pushed via postTheme so the
    // document is not rebuilt (which would reset interactive widget state).
    iframe.srcdoc = buildWidgetDoc(valueRef.current, varsToCss(collectThemeVars()));
  }

  // Coalesced re-render on streaming updates. First paint is immediate; during
  // an active stream we debounce so the frame is not reloaded on every token,
  // but never wait longer than RENDER_MAX_WAIT_MS so long streams keep showing
  // progress. The final (settled) value always produces an authoritative paint.
  useEffect(() => {
    const elapsed = Date.now() - lastRenderAt.current;
    const wait = elapsed >= RENDER_MAX_WAIT_MS
      ? 0
      : Math.min(RENDER_DEBOUNCE_MS, Math.max(0, RENDER_MAX_WAIT_MS - elapsed));
    clearTimeout(renderTimer.current);
    renderTimer.current = window.setTimeout(renderNow, wait);
    return () => clearTimeout(renderTimer.current);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [value]);

  // Host → widget message channel (height, prompt request, link request). We
  // authenticate the source by contentWindow; the payload itself is untrusted.
  useEffect(() => {
    let heightTimer = 0;
    let current = 0;
    const handlers: WidgetMessageHandlers = {
      setHeight: (px) => {
        if (px === current) return;
        current = px;
        clearTimeout(heightTimer);
        heightTimer = window.setTimeout(() => {
          const f = iframeRef.current;
          if (f) f.style.height = px + "px";
        }, 30);
      },
      requestPrompt: (text) => setPending((prev) => prev ?? { id: nextRequestId++, kind: "prompt", value: text }),
      requestLink: (url) => setPending((prev) => prev ?? { id: nextRequestId++, kind: "link", value: url }),
    };
    function onMsg(e: MessageEvent) {
      const iframe = iframeRef.current;
      if (!iframe || e.source !== iframe.contentWindow) return;
      routeWidgetMessage(e.data, handlers);
    }
    window.addEventListener("message", onMsg);
    return () => { window.removeEventListener("message", onMsg); clearTimeout(heightTimer); };
  }, []);

  // Re-sync theme variables when the host theme changes, via postMessage rather
  // than a document rebuild — this preserves the widget's interactive state
  // (form inputs, selected options, running animations) across theme switches.
  useEffect(() => {
    if (typeof document === "undefined") return;
    const observer = new MutationObserver(() => postTheme(iframeRef.current));
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["data-theme", "data-theme-style", "class"],
    });
    return () => observer.disconnect();
  }, []);

  const confirmPending = () => {
    const req = pending;
    if (!req) return;
    setPending(null);
    if (req.kind === "prompt") {
      window.dispatchEvent(new CustomEvent("widget-send-prompt", { detail: { text: req.value } }));
    } else {
      openExternal(req.value);
    }
  };

  return (
    <div className="widget-stream">
      <iframe
        ref={iframeRef}
        sandbox="allow-scripts"
        title="widget"
        onLoad={() => postTheme(iframeRef.current)}
        style={{ width: "100%", height: "100px", border: "none", display: "block", overflow: "hidden", background: "transparent" }}
      />
      {pending && (
        <div className="widget-confirm" role="alertdialog" aria-label="widget action confirmation">
          <span className="widget-confirm__label">
            {pending.kind === "prompt" ? "Widget wants to send a message" : "Widget wants to open"}
          </span>
          {pending.kind === "link" && (
            <span className="widget-confirm__origin">{new URL(pending.value).hostname}</span>
          )}
          <span
            className={"widget-confirm__value" + (expanded ? " widget-confirm__value--expanded" : "")}
            title={pending.value}
            onClick={() => setExpanded(!expanded)}
          >{pending.value}</span>
          <div className="widget-confirm__actions">
            <button type="button" className="widget-confirm__go" onClick={confirmPending}>
              {pending.kind === "prompt" ? "Send" : "Open"}
            </button>
            <button type="button" className="widget-confirm__dismiss" onClick={() => setPending(null)}>
              Dismiss
            </button>
          </div>
        </div>
      )}
    </div>
  );
});
