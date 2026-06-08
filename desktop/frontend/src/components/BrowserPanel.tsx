import { useCallback, useEffect, useRef, useState } from "react";
import { ArrowLeft, ArrowRight, ExternalLink, Globe, Loader2, RefreshCw, RotateCcw } from "lucide-react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import type { DockTab } from "../lib/types";

interface BrowserPanelProps {
  tab: DockTab;
  onMetadataUpdate: (id: string, meta: Record<string, unknown>) => void;
  onTitleUpdate: (id: string, title: string) => void;
}

function normalizeURL(raw: string): string {
  const trimmed = raw.trim();
  if (!trimmed) return "";
  if (/^https?:\/\//i.test(trimmed)) return trimmed;
  // Support common localhost/private patterns without a protocol.
  if (/^localhost[:\/]|^127\.|^0\.|^10\.|^172\.(1[6-9]|2\d|3[01])\.|^192\.168\./.test(trimmed)) {
    return `http://${trimmed}`;
  }
  return `https://${trimmed}`;
}

const DEFAULT_HOMEPAGE = "about:blank";

export function BrowserPanel({ tab, onMetadataUpdate, onTitleUpdate }: BrowserPanelProps) {
  const t = useT();

  const meta = (tab.metadata ?? { url: DEFAULT_HOMEPAGE, history: [], historyIndex: -1, isLoading: false }) as {
    url: string;
    history: string[];
    historyIndex: number;
    isLoading: boolean;
  };

  const [urlInput, setUrlInput] = useState(meta.url === DEFAULT_HOMEPAGE ? "" : meta.url);
  const [currentUrl, setCurrentUrl] = useState(meta.url);
  const [isLoading, setIsLoading] = useState(false);
  const [isCdpReady, setIsCdpReady] = useState(false);
  const iframeRef = useRef<HTMLIFrameElement>(null);
  const currentUrlRef = useRef(currentUrl);

  useEffect(() => {
    currentUrlRef.current = currentUrl;
  }, [currentUrl]);

  // Check CDP readiness on mount.
  useEffect(() => {
    app.BrowserIsRunning().then(setIsCdpReady).catch(() => {});
  }, []);

  // --- Navigation (iframe-based) ---

  const navigateTo = useCallback(
    (target: string) => {
      const normalized = normalizeURL(target);
      if (!normalized) return;
      setUrlInput(normalized);
      setCurrentUrl(normalized);
      setIsLoading(true);
      onMetadataUpdate(tab.id, { url: normalized, isLoading: true });
      // Also tell the CDP backend so AI can follow along.
      app.BrowserNavigate(normalized).catch(() => {});
    },
    [tab.id, onMetadataUpdate],
  );

  const handleGo = useCallback(() => {
    navigateTo(urlInput);
  }, [navigateTo, urlInput]);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLInputElement>) => {
      if (e.key === "Enter") {
        e.preventDefault();
        navigateTo(urlInput);
      }
    },
    [navigateTo, urlInput],
  );

  const handleRefresh = useCallback(() => {
    if (iframeRef.current && currentUrl !== DEFAULT_HOMEPAGE) {
      setIsLoading(true);
      iframeRef.current.src = currentUrl;
      app.BrowserRefresh().catch(() => {});
    }
  }, [currentUrl]);

  const handleBack = useCallback(() => {
    try {
      iframeRef.current?.contentWindow?.history.back();
    } catch { /* cross-origin */ }
    app.BrowserBack().catch(() => {});
  }, []);

  const handleForward = useCallback(() => {
    try {
      iframeRef.current?.contentWindow?.history.forward();
    } catch { /* cross-origin */ }
    app.BrowserForward().catch(() => {});
  }, []);

  const handleOpenInSystem = useCallback(() => {
    if (currentUrl && currentUrl !== DEFAULT_HOMEPAGE) {
      void app.OpenURL(currentUrl);
    }
  }, [currentUrl]);

  // --- Iframe load: sync URL bar + title when same-origin ---

  const handleIframeLoad = useCallback(() => {
    setIsLoading(false);
    onMetadataUpdate(tab.id, { isLoading: false });

    try {
      const iframe = iframeRef.current;
      if (!iframe?.contentWindow) return;

      const doc = iframe.contentDocument ?? iframe.contentWindow.document;
      const title = doc.title || "";
      const href = doc.URL || currentUrlRef.current;

      if (title) {
        onTitleUpdate(tab.id, title);
      }

      if (href && href !== DEFAULT_HOMEPAGE && href !== "about:blank" && href !== currentUrlRef.current) {
        setCurrentUrl(href);
        setUrlInput(href);
        onMetadataUpdate(tab.id, { url: href });
      }
    } catch {
      // Cross-origin — can't read doc props; that's normal.
    }
  }, [tab.id, onMetadataUpdate, onTitleUpdate]);

  // --- Open from external (metadata.openUrl trigger) ---

  useEffect(() => {
    const storedUrl = (tab.metadata as Record<string, unknown>)?.openUrl as string | undefined;
    if (storedUrl && storedUrl !== currentUrlRef.current) {
      const normalized = normalizeURL(storedUrl);
      if (normalized) {
        setUrlInput(normalized);
        setCurrentUrl(normalized);
        setIsLoading(true);
      }
      onMetadataUpdate(tab.id, { openUrl: undefined });
    }
  }, [tab.metadata, tab.id, onMetadataUpdate]);

  // Drive iframe src when currentUrl changes (avoids re-setting the same URL).
  useEffect(() => {
    if (currentUrl && currentUrl !== DEFAULT_HOMEPAGE && iframeRef.current) {
      try {
        const iframeDoc = iframeRef.current.contentDocument ?? iframeRef.current.contentWindow?.document;
        if (iframeDoc && iframeDoc.URL !== currentUrl) {
          iframeRef.current.src = currentUrl;
        }
      } catch {
        iframeRef.current.src = currentUrl;
      }
    }
  }, [currentUrl]);

  const showBrowser = currentUrl && currentUrl !== DEFAULT_HOMEPAGE;

  return (
    <div className="browser-panel">
      {/* ── Navigation bar ── */}
      <div className="browser-nav">
        <button className="browser-nav__btn" type="button" aria-label="Back" onClick={handleBack} title={t("rightDock.browserBack") || "Back"}>
          <ArrowLeft size={14} />
        </button>
        <button className="browser-nav__btn" type="button" aria-label="Forward" onClick={handleForward} title={t("rightDock.browserForward") || "Forward"}>
          <ArrowRight size={14} />
        </button>
        <button className="browser-nav__btn" type="button" aria-label="Refresh" onClick={handleRefresh} title={t("rightDock.browserRefresh") || "Refresh"}>
          {isLoading ? <Loader2 size={14} className="spinning" /> : <RefreshCw size={14} />}
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
        <button className="browser-nav__btn browser-nav__btn--go" type="button" aria-label={t("rightDock.browserOpen")} onClick={handleGo} title={t("rightDock.browserGo") || "Go"}>
          <Globe size={14} />
        </button>
        {showBrowser && (
          <button className="browser-nav__btn" type="button" aria-label="Open in system browser" onClick={handleOpenInSystem} title={t("rightDock.browserOpenExternal") || "Open in browser"}>
            <ExternalLink size={14} />
          </button>
        )}
      </div>

      {/* ── Iframe browser content ── */}
      <div className="browser-frame-container">
        {showBrowser ? (
          <iframe
            ref={iframeRef}
            className="browser-frame"
            src={currentUrl}
            onLoad={handleIframeLoad}
            sandbox="allow-same-origin allow-scripts allow-popups allow-forms"
            allow="cross-origin-isolated"
          />
        ) : (
          <div className="browser-empty">
            <div className="browser-empty__hint">{t("rightDock.browserEmpty")}</div>
          </div>
        )}

        {/* Loading overlay */}
        {isLoading && (
          <div className="browser-loading">
            <Loader2 size={20} className="spinning" />
          </div>
        )}

        {/* CDP status badge */}
        <div className={`browser-status ${isCdpReady ? "browser-status--online" : ""}`}>
          <RotateCcw size={10} />
          <span>CDP</span>
        </div>
      </div>
    </div>
  );
}
