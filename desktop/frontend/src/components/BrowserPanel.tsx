import { useCallback, useEffect, useRef, useState } from "react";
import type { FormEvent, KeyboardEvent as ReactKeyboardEvent } from "react";
import {
  ArrowLeft,
  ArrowRight,
  Check,
  Copy,
  ExternalLink,
  Globe,
  MessageSquare,
  MoreVertical,
  MousePointerClick,
  RefreshCw,
  ZoomIn,
  ZoomOut,
} from "lucide-react";
import { useT } from "../lib/i18n";
import { app, openExternal } from "../lib/bridge";
import {
  loadBrowserUrlForSession,
  normalizeBrowserUrl,
  rememberNativeBrowserUrl,
  sameNativeBrowserUrl,
  saveBrowserUrlForSession,
} from "../lib/browserUrl";
import { type BrowserAnnotationPayload, type BrowserAnnotationRect } from "../lib/browserAnnotation";
import { useOverlayStore } from "../store/overlays";
import { Tooltip } from "./Tooltip";

type PendingSelection = {
  kind: "element";
  rect: BrowserAnnotationRect;
  selector?: string;
  tagName?: string;
  text?: string;
};

type EmbedState = {
  url: string;
  title: string;
  canGoBack: boolean;
  canGoForward: boolean;
  loading?: boolean;
  engine?: string;
};

type EmbedPick = {
  x: number;
  y: number;
  width: number;
  height: number;
  selector?: string;
  tagName?: string;
  text?: string;
};

function readViewportClientBounds(el: HTMLElement | null): {
  x: number;
  y: number;
  width: number;
  height: number;
  screenX: number;
  screenY: number;
} | null {
  if (!el) return null;
  const rect = el.getBoundingClientRect();
  if (rect.width < 1 || rect.height < 1) return null;
  return {
    x: rect.left,
    y: rect.top,
    width: rect.width,
    height: rect.height,
    screenX: window.screenX + rect.left,
    screenY: window.screenY + rect.top,
  };
}

export function BrowserPanel({
  sessionKey,
  onAddAnnotation,
}: {
  /** Active chat/tab id — browser URL/history is scoped per session. */
  sessionKey: string;
  onAddAnnotation: (payload: BrowserAnnotationPayload) => void | Promise<void>;
  onClose?: () => void;
}) {
  const t = useT();
  const viewportRef = useRef<HTMLDivElement | null>(null);
  const sessionKeyRef = useRef(sessionKey);
  sessionKeyRef.current = sessionKey;
  const overlayBlocksNative = useOverlayStore((s) =>
    Boolean(
      s.paletteOpen ||
        s.settingsTarget != null ||
        s.shortcutsOpen ||
        s.heartbeatOpen ||
        s.topicExportOpen ||
        s.startupSplashVisible ||
        s.needsOnboarding === true ||
        s.imageViewerOpenCount > 0,
    ),
  );
  const [addressDraft, setAddressDraft] = useState(() => loadBrowserUrlForSession(sessionKey));
  const [currentUrl, setCurrentUrl] = useState("");
  const [addressFocused, setAddressFocused] = useState(false);
  const [engineReady, setEngineReady] = useState<boolean | null>(null);
  const [engineError, setEngineError] = useState("");
  const [engineName, setEngineName] = useState("");
  const [nav, setNav] = useState({ canGoBack: false, canGoForward: false, title: "" });
  const [annotating, setAnnotating] = useState(false);
  const [freezeFrame, setFreezeFrame] = useState<string>("");
  const [menuOpen, setMenuOpen] = useState(false);
  const [zoom, setZoom] = useState(1);
  const [pending, setPending] = useState<PendingSelection | null>(null);
  const [note, setNote] = useState("");
  const restoredRef = useRef(false);
  const handlingPickRef = useRef(false);

  const hasPage = Boolean(currentUrl);
  // Keep the native WebView live while picking elements. Only hide it after a
  // pick so the React describe form (under the sibling WKWebView) can receive input.
  const describing = annotating && pending != null;
  // Hide native WebView while any React chrome above the page needs clicks
  // (address bar, overflow menu). The sibling WKWebView otherwise steals hits.
  const nativeVisible =
    engineReady === true &&
    hasPage &&
    !describing &&
    !overlayBlocksNative &&
    !addressFocused &&
    !menuOpen;

  const syncBounds = useCallback(async () => {
    const bounds = readViewportClientBounds(viewportRef.current);
    if (!bounds) return;
    try {
      await app.EmbedBrowserSetBounds(bounds);
    } catch {
      /* ignore */
    }
  }, []);

  const stopPickMode = useCallback(async () => {
    try {
      await app.EmbedBrowserSetPickMode(false);
    } catch {
      /* ignore */
    }
  }, []);

  const readThemeAccent = useCallback(() => {
    if (typeof window === "undefined") {
      return { accent: "#d97757", accentFg: "#ffffff" };
    }
    const styles = getComputedStyle(document.documentElement);
    const accent = styles.getPropertyValue("--accent").trim() || "#d97757";
    const accentFg = styles.getPropertyValue("--accent-fg").trim() || "#ffffff";
    return { accent, accentFg };
  }, []);

  const startPickMode = useCallback(async () => {
    try {
      await syncBounds();
      const { accent, accentFg } = readThemeAccent();
      await app.EmbedBrowserSetPickMode(true, accent, accentFg);
    } catch {
      /* ignore */
    }
  }, [readThemeAccent, syncBounds]);

  useEffect(() => {
    let cancelled = false;
    void (async () => {
      try {
        const ok = await app.EmbedBrowserAvailable();
        if (!cancelled) setEngineReady(ok);
      } catch {
        if (!cancelled) setEngineReady(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    const runtime = (window as unknown as { runtime?: { EventsOn?: (name: string, cb: (...args: unknown[]) => void) => () => void } }).runtime;
    if (!runtime?.EventsOn) return;
    const offState = runtime.EventsOn("embed-browser:state", (payload) => {
      const state = payload as EmbedState;
      if (typeof state?.engine === "string" && state.engine) {
        setEngineName(state.engine);
      }
      if (typeof state?.url === "string" && state.url && state.url !== "about:blank") {
        rememberNativeBrowserUrl(state.url);
        setCurrentUrl(state.url);
        setAddressDraft((draft) => {
          // Never clobber in-progress address edits.
          if (document.activeElement?.closest?.(".browser-panel__address")) {
            return draft;
          }
          return state.url;
        });
        saveBrowserUrlForSession(sessionKeyRef.current, state.url);
      }
      setNav({
        canGoBack: Boolean(state?.canGoBack),
        canGoForward: Boolean(state?.canGoForward),
        title: state?.title ?? "",
      });
    });
    const offError = runtime.EventsOn("embed-browser:error", (payload) => {
      setEngineError(typeof payload === "string" ? payload : String(payload ?? ""));
    });
    const offPick = runtime.EventsOn("embed-browser:pick", (payload) => {
      const pick = payload as EmbedPick;
      if (handlingPickRef.current) return;
      handlingPickRef.current = true;
      void (async () => {
        try {
          // Clear in-page pick chrome (label/box) first so the freeze frame
          // does not bake the floating `.class "text"` chip into the screenshot.
          await stopPickMode();
          // Let the disable script paint before snapshot.
          await new Promise<void>((resolve) => {
            window.requestAnimationFrame(() => resolve());
          });
          let shot = "";
          try {
            shot = await app.EmbedBrowserSnapshotPNG();
          } catch {
            shot = "";
          }
          const rect: BrowserAnnotationRect = {
            x: Math.max(0, Number(pick?.x) || 0),
            y: Math.max(0, Number(pick?.y) || 0),
            width: Math.max(1, Number(pick?.width) || 1),
            height: Math.max(1, Number(pick?.height) || 1),
          };
          setFreezeFrame(shot);
          setNote("");
          setPending({
            kind: "element",
            rect,
            selector: typeof pick?.selector === "string" ? pick.selector : undefined,
            tagName: typeof pick?.tagName === "string" ? pick.tagName : undefined,
            text: typeof pick?.text === "string" ? pick.text : undefined,
          });
        } finally {
          handlingPickRef.current = false;
        }
      })();
    });
    return () => {
      offState?.();
      offError?.();
      offPick?.();
    };
  }, [stopPickMode]);

  useEffect(() => {
    if (!nativeVisible) {
      void app.EmbedBrowserHide();
      return;
    }
    void app.EmbedBrowserShow();
    void syncBounds();
    const onResize = () => { void syncBounds(); };
    window.addEventListener("resize", onResize);
    const ro = typeof ResizeObserver !== "undefined" ? new ResizeObserver(onResize) : null;
    if (viewportRef.current && ro) ro.observe(viewportRef.current);
    const timer = window.setInterval(() => { void syncBounds(); }, 500);
    return () => {
      window.removeEventListener("resize", onResize);
      ro?.disconnect();
      window.clearInterval(timer);
      void app.EmbedBrowserHide();
    };
  }, [nativeVisible, syncBounds]);

  useEffect(() => {
    return () => {
      void stopPickMode();
      void app.EmbedBrowserHide();
    };
  }, [stopPickMode]);

  // While picking, keep the in-page script armed whenever the native view is shown.
  useEffect(() => {
    if (!annotating || pending || !nativeVisible) return;
    void startPickMode();
  }, [annotating, pending, nativeVisible, startPickMode]);

  const commitNavigation = useCallback(async (raw: string) => {
    const next = normalizeBrowserUrl(raw);
    if (!next) return;
    setEngineError("");
    setPending(null);
    setNote("");
    setFreezeFrame("");
    setCurrentUrl(next);
    setAddressDraft(next);
    saveBrowserUrlForSession(sessionKeyRef.current, next);
    if (engineReady) {
      await syncBounds();
      await app.EmbedBrowserNavigate(next);
      await syncBounds();
    }
  }, [engineReady, syncBounds]);

  // Restore this session's URL once the native engine is ready.
  // New sessions start empty; switching back restores that session's page.
  useEffect(() => {
    if (engineReady !== true || restoredRef.current) return;
    const last = loadBrowserUrlForSession(sessionKey);
    if (!last) {
      restoredRef.current = true;
      setCurrentUrl("");
      setAddressDraft("");
      return;
    }
    restoredRef.current = true;
    if (sameNativeBrowserUrl(last)) {
      setCurrentUrl(last);
      setAddressDraft(last);
      return;
    }
    void commitNavigation(last);
  }, [engineReady, commitNavigation, sessionKey]);

  const onAddressSubmit = (event: FormEvent) => {
    event.preventDefault();
    setAddressFocused(false);
    void commitNavigation(addressDraft);
  };

  const goBack = () => { void app.EmbedBrowserGoBack(); };
  const goForward = () => { void app.EmbedBrowserGoForward(); };
  const hardReload = () => { void app.EmbedBrowserReload(); };

  const copyUrl = async () => {
    if (!currentUrl) return;
    try {
      await navigator.clipboard.writeText(currentUrl);
    } catch {
      /* ignore */
    }
    setMenuOpen(false);
  };

  const openInSystemBrowser = () => {
    if (!currentUrl) return;
    openExternal(currentUrl);
    setMenuOpen(false);
  };

  const setZoomFactor = async (next: number) => {
    const clamped = Math.min(2, Math.max(0.5, Number(next.toFixed(2))));
    setZoom(clamped);
    try {
      await app.EmbedBrowserSetZoom(clamped);
    } catch {
      /* ignore */
    }
  };

  const exitAnnotate = async () => {
    setAnnotating(false);
    setFreezeFrame("");
    setPending(null);
    setNote("");
    await stopPickMode();
  };

  const startAnnotate = async () => {
    if (annotating) {
      await exitAnnotate();
      return;
    }
    setMenuOpen(false);
    setPending(null);
    setNote("");
    setFreezeFrame("");
    setAnnotating(true);
    // Pick mode is armed by the effect once nativeVisible is true.
  };

  const cancelPending = () => {
    setPending(null);
    setNote("");
    setFreezeFrame("");
    // Stay in annotate mode — re-enter pick once native is shown again.
  };

  const submitAnnotation = async () => {
    if (!pending) return;
    const trimmedNote = note.trim();
    if (!trimmedNote) return;
    await onAddAnnotation({
      url: currentUrl || "about:blank",
      note: trimmedNote,
      rect: pending.rect,
      selector: pending.selector,
      tagName: pending.tagName,
      text: pending.text,
      screenshotDataUrl: freezeFrame || undefined,
    });
    await exitAnnotate();
  };

  const onNoteKeyDown = (event: ReactKeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      void submitAnnotation();
    }
    if (event.key === "Escape") {
      event.preventDefault();
      cancelPending();
    }
  };

  const viewportW = viewportRef.current?.clientWidth ?? 320;
  const viewportH = viewportRef.current?.clientHeight ?? 240;
  const describeWidth = Math.min(340, Math.max(260, viewportW - 24));
  const describeLeft = pending
    ? Math.min(
      Math.max(8, pending.rect.x + Math.max(pending.rect.width, 8) / 2 - describeWidth / 2),
      Math.max(8, viewportW - describeWidth - 8),
    )
    : 8;
  const spaceBelow = pending
    ? viewportH - (pending.rect.y + Math.max(pending.rect.height, 8)) - 16
    : 0;
  const describeAbove = Boolean(pending && spaceBelow < 72);
  const describeTop = pending
    ? describeAbove
      ? Math.max(8, pending.rect.y - 64)
      : Math.min(pending.rect.y + Math.max(pending.rect.height, 8) + 10, Math.max(8, viewportH - 72))
    : 8;

  const engineLabel = (engineName || engineReady)
    ? t("browser.nativeEngine")
    : t("browser.emptyTitle");

  return (
    <div className="browser-panel">
      <div className="browser-panel__toolbar">
        <div className="browser-panel__nav">
          <Tooltip label={t("browser.back")}>
            <button type="button" className="browser-panel__icon-btn" disabled={!nav.canGoBack} onClick={goBack} aria-label={t("browser.back")}>
              <ArrowLeft size={14} />
            </button>
          </Tooltip>
          <Tooltip label={t("browser.forward")}>
            <button type="button" className="browser-panel__icon-btn" disabled={!nav.canGoForward} onClick={goForward} aria-label={t("browser.forward")}>
              <ArrowRight size={14} />
            </button>
          </Tooltip>
          <Tooltip label={t("browser.reload")}>
            <button type="button" className="browser-panel__icon-btn" disabled={!hasPage || engineReady !== true} onClick={hardReload} aria-label={t("browser.reload")}>
              <RefreshCw size={14} />
            </button>
          </Tooltip>
        </div>
        <form className="browser-panel__address" onSubmit={onAddressSubmit}>
          <Globe size={13} aria-hidden="true" />
          <input
            value={addressDraft}
            onChange={(e) => setAddressDraft(e.target.value)}
            onFocus={() => setAddressFocused(true)}
            onBlur={() => setAddressFocused(false)}
            spellCheck={false}
            autoCapitalize="off"
            autoCorrect="off"
            placeholder="https://"
            aria-label={t("browser.address")}
          />
        </form>
        <button
          type="button"
          className={`browser-panel__annotate${annotating ? " browser-panel__annotate--active" : ""}`}
          disabled={engineReady !== true || !hasPage}
          onClick={() => { void startAnnotate(); }}
        >
          <MousePointerClick size={14} />
          <span>{annotating ? t("browser.annotating") : t("browser.annotate")}</span>
        </button>
        <div className="browser-panel__menu-wrap">
          <Tooltip label={t("browser.menu")}>
            <button
              type="button"
              className="browser-panel__icon-btn"
              aria-label={t("browser.menu")}
              aria-expanded={menuOpen}
              onClick={() => setMenuOpen((open) => !open)}
            >
              <MoreVertical size={14} />
            </button>
          </Tooltip>
          {menuOpen && (
            <div className="browser-panel__menu" role="menu">
              <div className="browser-panel__menu-zoom">
                <span>{t("browser.zoom")}</span>
                <button type="button" onClick={() => { void setZoomFactor(zoom - 0.1); }} aria-label={t("browser.zoomOut")}>
                  <ZoomOut size={13} />
                </button>
                <span>{Math.round(zoom * 100)}%</span>
                <button type="button" onClick={() => { void setZoomFactor(zoom + 0.1); }} aria-label={t("browser.zoomIn")}>
                  <ZoomIn size={13} />
                </button>
                <button type="button" onClick={() => { void setZoomFactor(1); }}>{t("browser.zoomReset")}</button>
              </div>
              <button type="button" role="menuitem" onClick={() => { void copyUrl(); }}>
                <Copy size={13} />
                {t("browser.copyUrl")}
              </button>
              <button type="button" role="menuitem" onClick={() => { hardReload(); setMenuOpen(false); }}>
                <RefreshCw size={13} />
                {t("browser.hardReload")}
              </button>
              <button type="button" role="menuitem" onClick={openInSystemBrowser}>
                <ExternalLink size={13} />
                {t("browser.openExternal")}
              </button>
            </div>
          )}
        </div>
      </div>

      <div className={`browser-panel__viewport${nativeVisible ? " browser-panel__viewport--native" : ""}`} ref={viewportRef}>
        {engineReady === false ? (
          <div className="browser-panel__empty">
            <Globe size={28} aria-hidden="true" />
            <p className="browser-panel__empty-title">{t("browser.nativeMissingTitle")}</p>
            <p className="browser-panel__empty-hint">{t("browser.nativeMissingHint")}</p>
          </div>
        ) : !hasPage ? (
          <div className="browser-panel__empty">
            <Globe size={28} aria-hidden="true" />
            <p className="browser-panel__empty-title">{t("browser.emptyTitle")}</p>
          </div>
        ) : describing ? (
          <div
            className="browser-panel__freeze"
            style={freezeFrame ? { backgroundImage: `url(${freezeFrame})` } : undefined}
          />
        ) : annotating ? (
          <div className="browser-panel__native-slot browser-panel__native-slot--picking" aria-hidden="true" />
        ) : (
          <div className="browser-panel__native-slot" aria-hidden="true" />
        )}

        {describing && pending && (
          <>
            <button
              type="button"
              className="browser-panel__describe-backdrop"
              aria-label={t("browser.cancel")}
              onClick={cancelPending}
            />
            <div
              className="browser-panel__highlight browser-panel__highlight--selected"
              style={{
                left: pending.rect.x,
                top: pending.rect.y,
                width: Math.max(pending.rect.width, 8),
                height: Math.max(pending.rect.height, 8),
              }}
            >
              <span className="browser-panel__highlight-handle browser-panel__highlight-handle--nw" />
              <span className="browser-panel__highlight-handle browser-panel__highlight-handle--ne" />
              <span className="browser-panel__highlight-handle browser-panel__highlight-handle--sw" />
              <span className="browser-panel__highlight-handle browser-panel__highlight-handle--se" />
            </div>
            <div
              className={`browser-panel__describe${describeAbove ? " browser-panel__describe--above" : ""}`}
              style={{ left: describeLeft, top: describeTop, width: describeWidth }}
              role="dialog"
              aria-label={t("browser.addToChat")}
            >
              <div className="browser-panel__describe-row">
                <span className="browser-panel__describe-icon" aria-hidden="true">
                  <MessageSquare size={14} />
                </span>
                <textarea
                  value={note}
                  onChange={(e) => setNote(e.target.value)}
                  onKeyDown={onNoteKeyDown}
                  placeholder={t("browser.describePlaceholder")}
                  rows={1}
                  autoFocus
                />
                <button
                  type="button"
                  className="browser-panel__describe-cancel"
                  onClick={cancelPending}
                >
                  {t("browser.cancel")}
                </button>
                <button
                  type="button"
                  className="browser-panel__describe-submit"
                  disabled={!note.trim()}
                  onClick={() => void submitAnnotation()}
                  aria-label={t("browser.addToChat")}
                  title={t("browser.addToChat")}
                >
                  <Check size={14} strokeWidth={2.5} />
                </button>
              </div>
            </div>
          </>
        )}
      </div>

      <div className="browser-panel__status">
        <span>{engineLabel}</span>
        {annotating && !pending ? <span>{t("browser.annotating")}</span> : null}
        {nav.title ? <span>{nav.title}</span> : null}
        {engineError ? <span className="browser-panel__status-error">{engineError}</span> : null}
      </div>

      {menuOpen && (
        <button type="button" className="browser-panel__menu-backdrop" aria-label={t("browser.menuClose")} onClick={() => setMenuOpen(false)} />
      )}
    </div>
  );
}
