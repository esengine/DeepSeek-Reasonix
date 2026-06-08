import { useCallback, useRef, useState } from "react";
import { ArrowLeft, ArrowRight, RefreshCw } from "lucide-react";
import { useT } from "../lib/i18n";
import type { DockTab } from "../lib/types";

interface BrowserPanelProps {
  tab: DockTab;
  onMetadataUpdate: (id: string, meta: Record<string, unknown>) => void;
  onTitleUpdate: (id: string, title: string) => void;
}

export function BrowserPanel({ tab, onMetadataUpdate, onTitleUpdate }: BrowserPanelProps) {
  const t = useT();
  const meta = (tab.metadata ?? { url: "about:blank", history: [], historyIndex: -1, isLoading: false }) as {
    url: string;
    history: string[];
    historyIndex: number;
    isLoading: boolean;
  };
  const [urlInput, setUrlInput] = useState(meta.url === "about:blank" ? "" : meta.url);
  const iframeRef = useRef<HTMLIFrameElement>(null);

  const navigate = useCallback(
    (targetUrl: string) => {
      let normalized = targetUrl.trim();
      if (normalized && !/^https?:\/\//i.test(normalized)) {
        normalized = `https://${normalized}`;
      }
      if (!normalized) return;
      setUrlInput(normalized);
      const newHistory = meta.history.slice(0, meta.historyIndex + 1);
      newHistory.push(normalized);
      onMetadataUpdate(tab.id, {
        url: normalized,
        history: newHistory,
        historyIndex: newHistory.length - 1,
        isLoading: true,
      });
      try {
        const host = new URL(normalized).hostname;
        onTitleUpdate(tab.id, host);
      } catch {
        // ignore
      }
    },
    [meta.history, meta.historyIndex, tab.id, onMetadataUpdate, onTitleUpdate],
  );

  const goBack = useCallback(() => {
    if (meta.historyIndex <= 0) return;
    const idx = meta.historyIndex - 1;
    const url = meta.history[idx];
    setUrlInput(url);
    onMetadataUpdate(tab.id, { url, historyIndex: idx, isLoading: true });
  }, [meta.history, meta.historyIndex, tab.id, onMetadataUpdate]);

  const goForward = useCallback(() => {
    if (meta.historyIndex >= meta.history.length - 1) return;
    const idx = meta.historyIndex + 1;
    const url = meta.history[idx];
    setUrlInput(url);
    onMetadataUpdate(tab.id, { url, historyIndex: idx, isLoading: true });
  }, [meta.history, meta.historyIndex, tab.id, onMetadataUpdate]);

  const reload = useCallback(() => {
    if (iframeRef.current && meta.url !== "about:blank") {
      iframeRef.current.src = meta.url;
    }
  }, [meta.url]);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLInputElement>) => {
      if (e.key === "Enter") {
        e.preventDefault();
        navigate(urlInput);
      }
    },
    [navigate, urlInput],
  );

  const handleIframeLoad = useCallback(() => {
    onMetadataUpdate(tab.id, { isLoading: false });
    try {
      const doc = iframeRef.current?.contentDocument;
      if (doc?.title) {
        onTitleUpdate(tab.id, doc.title);
      }
    } catch {
      // Cross-origin: fall back to hostname
    }
  }, [tab.id, onMetadataUpdate, onTitleUpdate]);

  const handleIframeError = useCallback(() => {
    onMetadataUpdate(tab.id, { isLoading: false });
  }, [tab.id, onMetadataUpdate]);

  const canGoBack = meta.historyIndex > 0;
  const canGoForward = meta.historyIndex < meta.history.length - 1;

  return (
    <div className="browser-panel">
      <div className="browser-nav">
        <button
          className="browser-nav__btn"
          type="button"
          disabled={!canGoBack}
          aria-label={t("rightDock.browserBack")}
          onClick={goBack}
        >
          <ArrowLeft size={14} />
        </button>
        <button
          className="browser-nav__btn"
          type="button"
          disabled={!canGoForward}
          aria-label={t("rightDock.browserForward")}
          onClick={goForward}
        >
          <ArrowRight size={14} />
        </button>
        <button
          className="browser-nav__btn"
          type="button"
          aria-label={t("rightDock.browserReload")}
          onClick={reload}
        >
          <RefreshCw size={14} className={meta.isLoading ? "spinning" : ""} />
        </button>
        <div className="browser-url">
          <input
            className="browser-url__input"
            type="text"
            value={urlInput}
            onChange={(e) => setUrlInput(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder={t("rightDock.browserUrlPlaceholder")}
          />
        </div>
      </div>
      <div className="browser-frame-container">
        {meta.url && meta.url !== "about:blank" ? (
          <iframe
            ref={iframeRef}
            className="browser-frame"
            src={meta.url}
            onLoad={handleIframeLoad}
            onError={handleIframeError}
            title={tab.title}
          />
        ) : (
          <div className="browser-empty">
            <div className="browser-empty__hint">{t("rightDock.browserEmpty")}</div>
          </div>
        )}
      </div>
    </div>
  );
}
