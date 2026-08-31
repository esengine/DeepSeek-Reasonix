// BrowserPanel embeds a web page in the right dock with an address bar and
// back/forward/reload navigation. Wails allows <webview> via the dedicated
// element; in plain browser dev it falls back to an <iframe> so the panel
// stays usable. The address bar is intentionally minimal: a URL input,
// go/enter to navigate, and back/forward/reload buttons.

import { useCallback, useRef, useState } from "react";
import { ArrowLeft, ArrowRight, RotateCw } from "lucide-react";
import { useT } from "../lib/i18n";

const DEFAULT_HOME = "https://example.com";

function normalizeUrl(raw: string): string {
  const trimmed = raw.trim();
  if (!trimmed) return DEFAULT_HOME;
  if (/^https?:\/\//i.test(trimmed)) return trimmed;
  return `https://${trimmed}`;
}

export function BrowserPanel() {
  const t = useT();
  const [url, setUrl] = useState(DEFAULT_HOME);
  const [addressDraft, setAddressDraft] = useState(DEFAULT_HOME);
  const [history, setHistory] = useState<string[]>([DEFAULT_HOME]);
  const [historyIndex, setHistoryIndex] = useState(0);
  const frameRef = useRef<HTMLIFrameElement | null>(null);

  const currentUrl = history[historyIndex] ?? DEFAULT_HOME;

  const navigate = useCallback((nextUrl: string) => {
    const normalized = normalizeUrl(nextUrl);
    setUrl(normalized);
    setAddressDraft(normalized);
    setHistory((prev) => [...prev.slice(0, historyIndex + 1), normalized]);
    setHistoryIndex((prev) => prev + 1);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [historyIndex]);

  const goBack = useCallback(() => {
    if (historyIndex <= 0) return;
    setHistoryIndex((prev) => prev - 1);
    setUrl(history[historyIndex - 1]);
    setAddressDraft(history[historyIndex - 1]);
  }, [history, historyIndex]);

  const goForward = useCallback(() => {
    if (historyIndex >= history.length - 1) return;
    setHistoryIndex((prev) => prev + 1);
    setUrl(history[historyIndex + 1]);
    setAddressDraft(history[historyIndex + 1]);
  }, [history, historyIndex]);

  const reload = useCallback(() => {
    const frame = frameRef.current;
    if (frame) {
      frame.src = currentUrl;
    }
  }, [currentUrl]);

  return (
    <div className="browser-panel">
      <div className="browser-panel__toolbar">
        <button type="button" className="browser-panel__nav" aria-label={t("browser.back")} onClick={goBack} disabled={historyIndex <= 0}>
          <ArrowLeft size={14} />
        </button>
        <button type="button" className="browser-panel__nav" aria-label={t("browser.forward")} onClick={goForward} disabled={historyIndex >= history.length - 1}>
          <ArrowRight size={14} />
        </button>
        <button type="button" className="browser-panel__nav" aria-label={t("browser.reload")} onClick={reload}>
          <RotateCw size={14} />
        </button>
        <form
          className="browser-panel__address"
          onSubmit={(event) => {
            event.preventDefault();
            navigate(addressDraft);
          }}
        >
          <input
            value={addressDraft}
            onChange={(event) => setAddressDraft(event.target.value)}
            onFocus={(event) => event.target.select()}
            placeholder={t("browser.addressPlaceholder")}
            spellCheck={false}
            aria-label={t("browser.addressPlaceholder")}
          />
        </form>
      </div>
      <div className="browser-panel__frame">
        <iframe
          ref={frameRef}
          key={url}
          src={url}
          title={t("browser.frameTitle")}
          sandbox="allow-scripts allow-same-origin allow-forms allow-popups"
        />
      </div>
    </div>
  );
}
