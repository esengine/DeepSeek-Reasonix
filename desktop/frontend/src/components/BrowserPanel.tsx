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
  // Support common localhost patterns without a protocol.
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
  const [canGoBack, setCanGoBack] = useState(false);
  const [canGoForward, setCanGoForward] = useState(false);
  const iframeRef = useRef<HTMLIFrameElement>(null);
  const currentUrlRef = useRef(currentUrl);

  // Keep ref in sync.
  useEffect(() => {
    currentUrlRef.current = currentUrl;
  }, [currentUrl]);

  // --- Navigation helpers ---

  const navigateTo = useCallback(
    (target: string) => {
      const normalized = normalizeURL(target);
      if (!normalized) return;
      setUrlInput(normalized);
      setCurrentUrl(normalized);
      setIsLoading(true);
      onMetadataUpdate(tab.id, { url: normalized, isLoading: true });
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
    }
  }, [currentUrl]);

  const handleBack = useCallback(() => {
    try {
      iframeRef.current?.contentWindow?.history.back();
    } catch {
      // Cross-origin iframe will throw — silently degrade.
    }
    // For the Go backend, also try backend navigation if available.
    app.BrowserBack().catch(() => {});
  }, []);

  const handleForward = useCallback(() => {
    try {
      iframeRef.current?.contentWindow?.history.forward();
    } catch {
      // Cross-origin iframe will throw — silently degrade.
    }
    app.BrowserForward().catch(() => {});
  }, []);

  const handleOpenInSystem = useCallback(() => {
    if (currentUrl && currentUrl !== DEFAULT_HOMEPAGE) {
      void app.OpenURL(currentUrl);
    }
  }, [currentUrl]);

  // --- Iframe lifecycle ---

  const handleIframeLoad = useCallback(() => {
    setIsLoading(false);
    onMetadataUpdate(tab.id, { isLoading: false });

    // Try to read the iframe's current URL and title.
    try {
      const iframe = iframeRef.current;
      if (!iframe?.contentWindow) return;

      const doc = iframe.contentDocument ?? iframe.contentWindow.document;
      const title = doc.title || "Untitled";
      const href = doc.URL || currentUrlRef.current;

      if (href && href !== DEFAULT_HOMEPAGE && href !== "about:blank") {
        setCurrentUrl(href);
        setUrlInput(href);
        onMetadataUpdate(tab.id, { url: href });
      }
      if (title) {
        onTitleUpdate(tab.id, title);
      }
    } catch {
      // Cross-origin — can't read document props. That's fine.
    }
  }, [tab.id, onMetadataUpdate, onTitleUpdate]);

  // --- Open from external (e.g., user clicked a link in chat) ---

  useEffect(() => {
    const storedUrl = (tab.metadata as Record<string, unknown>)?.openUrl as string | undefined;
    if (storedUrl && storedUrl !== currentUrlRef.current) {
      const normalized = normalizeURL(storedUrl);
      if (normalized) {
        setUrlInput(normalized);
        setCurrentUrl(normalized);
        setIsLoading(true);
      }
      // Clear the openUrl so we don't re-navigate on re-render.
      onMetadataUpdate(tab.id, { openUrl: undefined });
    }
  }, [tab.metadata, tab.id, onMetadataUpdate]);

  // Update the iframe src when currentUrl changes.
  useEffect(() => {
    if (currentUrl && currentUrl !== DEFAULT_HOMEPAGE && iframeRef.current) {
      // Only set src if it's different from the iframe's current src to avoid loops.
      try {
        const iframeDoc = iframeRef.current.contentDocument ?? iframeRef.current.contentWindow?.document;
        if (iframeDoc?.URL !== currentUrl) {
          iframeRef.current.src = currentUrl;
        }
      } catch {
        // Cross-origin — just set it anyway.
        iframeRef.current.src = currentUrl;
      }
    }
  }, [currentUrl]);

  const showBrowser = currentUrl && currentUrl !== DEFAULT_HOMEPAGE;

  return (
    <div className="browser-panel">
      {/* Navigation bar */}
      <div className="browser-nav">
        <button
          className="browser-nav__btn"
          type="button"
          aria-label="Back"
          onClick={handleBack}
          disabled={!canGoBack}
          title={t("rightDock.browserBack") || "Back"}
        >
          <ArrowLeft size={14} />
        </button>
        <button
          className="browser-nav__btn"
          type="button"
          aria-label="Forward"
          onClick={handleForward}
          disabled={!canGoForward}
          title={t("rightDock.browserForward") || "Forward"}
        >
          <ArrowRight size={14} />
        </button>
        <button
          className="browser-nav__btn"
          type="button"
          aria-label="Refresh"
          onClick={handleRefresh}
          title={t("rightDock.browserRefresh") || "Refresh"}
        >
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
        <button
          className="browser-nav__btn browser-nav__btn--go"
          type="button"
          aria-label={t("rightDock.browserOpen")}
          onClick={handleGo}
          title={t("rightDock.browserGo") || "Go"}
        >
          <Globe size={14} />
        </button>
        {showBrowser && (
          <button
            className="browser-nav__btn"
            type="button"
            aria-label="Open in system browser"
            onClick={handleOpenInSystem}
            title={t("rightDock.browserOpenExternal") || "Open in browser"}
          >
            <ExternalLink size={14} />
          </button>
        )}
      </div>

      {/* Browser content */}
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

        {/* Backend connection status */}
        {showBrowser && !meta.isLoading && (
          <div className="browser-status">
            <RotateCcw size={10} />
            <span>CDP</span>
          </div>
        )}
      </div>
    </div>
  );
}
