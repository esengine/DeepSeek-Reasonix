import {
  AlertCircle,
  ArrowLeft,
  ArrowRight,
  Crosshair,
  Globe2,
  LoaderCircle,
  RefreshCw,
} from "lucide-react";
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type CompositionEvent,
  type KeyboardEvent as ReactKeyboardEvent,
  type PointerEvent as ReactPointerEvent,
  type SyntheticEvent,
  type WheelEvent as ReactWheelEvent,
} from "react";

import {
  app,
  onBrowserExit,
  onBrowserFrame,
  onBrowserSelection,
  onBrowserState,
  type BrowserElementView,
  type BrowserFrameEvent,
  type BrowserSessionView,
} from "../lib/bridge";
import { createBrowserAnnotation, formatBrowserAnnotation } from "../lib/browserAnnotation";
import {
  browserElementBoxToCanvas,
  browserFloatingPosition,
  browserKeyEvent,
  browserModifiers,
  browserMouseButton,
  browserPointFromClient,
  browserViewportSize,
} from "../lib/browserInput";
import { useT } from "../lib/i18n";
import type { ComposerInsertRequest } from "../lib/types";
import { BrowserStyleInspector } from "./BrowserStyleInspector";

interface BrowserPanelProps {
  tabId: string;
  onInsertAnnotation?: (request: Omit<ComposerInsertRequest, "id">) => void;
}

type BrowserStatus = "starting" | "ready" | "loading" | "unavailable" | "error" | "exited";

function displayedURL(url: string): string {
  return url === "about:blank" ? "" : url;
}

function messageFrom(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

export function BrowserPanel({ tabId, onInsertAnnotation }: BrowserPanelProps) {
  const t = useT();
  const canvasRef = useRef<HTMLDivElement>(null);
  const keyboardRef = useRef<HTMLTextAreaElement>(null);
  const pageIdRef = useRef("");
  const sessionRef = useRef<BrowserSessionView | null>(null);
  const stateSequenceRef = useRef(0);
  const frameSequenceRef = useRef(0);
  const selectionSequenceRef = useRef(0);
  const hoverGenerationRef = useRef(0);
  const hoveredNodeIdRef = useRef(0);
  const startGenerationRef = useRef(0);
  const styleApplyGenerationRef = useRef(0);
  const addressEditingRef = useRef(false);
  const composingRef = useRef(false);
  const inspectingRef = useRef(false);
  const resizeFrameRef = useRef<number | null>(null);
  const hoverTimerRef = useRef<number | null>(null);
  const pointerMoveFrameRef = useRef<number | null>(null);
  const pendingPointerMoveRef = useRef<{
    x: number;
    y: number;
    buttons: number;
    modifiers: number;
  } | null>(null);

  const [status, setStatus] = useState<BrowserStatus>("starting");
  const [detail, setDetail] = useState("");
  const [address, setAddress] = useState("");
  const [session, setSession] = useState<BrowserSessionView | null>(null);
  const [frame, setFrame] = useState<BrowserFrameEvent | null>(null);
  const [inspecting, setInspecting] = useState(false);
  const [hoveredElement, setHoveredElement] = useState<BrowserElementView | null>(null);
  const [selection, setSelection] = useState<BrowserElementView | null>(null);
  const [canvasSize, setCanvasSize] = useState({ width: 1, height: 1 });
  const [annotationText, setAnnotationText] = useState("");
  const [inspectorApplying, setInspectorApplying] = useState(false);
  const [inspectorError, setInspectorError] = useState("");

  const applySession = useCallback((next: BrowserSessionView) => {
    pageIdRef.current = next.pageId;
    sessionRef.current = next;
    stateSequenceRef.current = Math.max(stateSequenceRef.current, next.sequence || 0);
    setSession(next);
    if (!addressEditingRef.current) setAddress(displayedURL(next.url));
  }, []);

  const setInspectMode = useCallback((active: boolean) => {
    inspectingRef.current = active;
    hoverGenerationRef.current += 1;
    hoveredNodeIdRef.current = 0;
    setInspecting(active);
    if (!active) setHoveredElement(null);
    if (active) window.requestAnimationFrame(() => canvasRef.current?.focus());
  }, []);

  const runNavigation = useCallback(async (operation: () => Promise<BrowserSessionView>) => {
    setStatus("loading");
    setDetail("");
    styleApplyGenerationRef.current += 1;
    setInspectorApplying(false);
    setInspectMode(false);
    setSelection(null);
    setAnnotationText("");
    setInspectorError("");
    try {
      const next = await operation();
      applySession(next);
      setStatus("ready");
    } catch (error) {
      setStatus("error");
      setDetail(messageFrom(error));
    }
  }, [applySession, setInspectMode]);

  const start = useCallback(async () => {
    const generation = ++startGenerationRef.current;
    setStatus("starting");
    setDetail("");
    try {
      const runtime = await app.BrowserRuntimeInfo();
      if (generation !== startGenerationRef.current) return;
      if (!runtime.available) {
        setStatus("unavailable");
        setDetail(runtime.error || t("browser.unavailable"));
        return;
      }

      const rect = canvasRef.current?.getBoundingClientRect();
      const viewport = browserViewportSize(rect?.width || 1280, rect?.height || 800);
      let next = await app.BrowserOpen(tabId, "about:blank", viewport.width, viewport.height);
      if (generation !== startGenerationRef.current) return;
      applySession(next);
      if (next.width !== viewport.width || next.height !== viewport.height) {
        next = await app.BrowserResize(tabId, next.pageId, viewport.width, viewport.height);
        if (generation !== startGenerationRef.current) return;
        applySession(next);
      }
      await app.BrowserStartScreencast(tabId, next.pageId);
      if (generation !== startGenerationRef.current) return;
      setStatus("ready");
      window.requestAnimationFrame(() => keyboardRef.current?.focus());
    } catch (error) {
      if (generation !== startGenerationRef.current) return;
      setStatus("error");
      setDetail(messageFrom(error));
    }
  }, [applySession, t, tabId]);

  useEffect(() => {
    const unsubscribeFrame = onBrowserFrame((event) => {
      if (event.tabId !== tabId || event.pageId !== pageIdRef.current) return;
      if (event.sequence <= frameSequenceRef.current) return;
      frameSequenceRef.current = event.sequence;
      setFrame(event);
      setStatus("ready");
    });
    const unsubscribeState = onBrowserState((event) => {
      if (event.tabId !== tabId) return;
      if (pageIdRef.current && event.pageId !== pageIdRef.current) return;
      if (event.sequence < stateSequenceRef.current) return;
      applySession(event);
    });
    const unsubscribeSelection = onBrowserSelection((event) => {
      if (event.tabId !== tabId || event.pageId !== pageIdRef.current) return;
      if (event.sequence <= selectionSequenceRef.current) return;
      selectionSequenceRef.current = event.sequence;
      setSelection(event.selection ?? null);
      setInspectorError("");
    });
    const unsubscribeExit = onBrowserExit((event) => {
      if (event.tabId !== tabId || event.pageId !== pageIdRef.current) return;
      pageIdRef.current = "";
      sessionRef.current = null;
      setSession(null);
      setFrame(null);
      setInspectMode(false);
      setSelection(null);
      setAnnotationText("");
      setStatus(event.error ? "error" : "exited");
      setDetail(event.error || "");
    });
    void start();
    return () => {
      startGenerationRef.current += 1;
      unsubscribeFrame();
      unsubscribeState();
      unsubscribeSelection();
      unsubscribeExit();
    };
  }, [applySession, setInspectMode, start, tabId]);

  useEffect(() => {
    if (session?.canAnnotate !== false) return;
    setInspectMode(false);
    setSelection(null);
    setAnnotationText("");
    setInspectorApplying(false);
    setInspectorError("");
  }, [session?.canAnnotate, setInspectMode]);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const observer = new ResizeObserver((entries) => {
      const entry = entries[0];
      if (!entry) return;
      setCanvasSize({
        width: Math.max(1, entry.contentRect.width),
        height: Math.max(1, entry.contentRect.height),
      });
      if (!pageIdRef.current) return;
      if (resizeFrameRef.current !== null) window.cancelAnimationFrame(resizeFrameRef.current);
      resizeFrameRef.current = window.requestAnimationFrame(() => {
        resizeFrameRef.current = null;
        const current = sessionRef.current;
        if (!current || !pageIdRef.current) return;
        const viewport = browserViewportSize(entry.contentRect.width, entry.contentRect.height);
        if (current.width === viewport.width && current.height === viewport.height) return;
        void app.BrowserResize(tabId, current.pageId, viewport.width, viewport.height)
          .then(applySession)
          .catch((error) => {
            setStatus("error");
            setDetail(messageFrom(error));
          });
      });
    });
    observer.observe(canvas);
    return () => {
      observer.disconnect();
      if (resizeFrameRef.current !== null) window.cancelAnimationFrame(resizeFrameRef.current);
      if (hoverTimerRef.current !== null) window.clearTimeout(hoverTimerRef.current);
      if (pointerMoveFrameRef.current !== null) window.cancelAnimationFrame(pointerMoveFrameRef.current);
    };
  }, [applySession, tabId]);

  const navigate = useCallback((event: SyntheticEvent<HTMLFormElement>) => {
    event.preventDefault();
    const pageId = pageIdRef.current;
    if (!pageId) return;
    addressEditingRef.current = false;
    void runNavigation(() => app.BrowserNavigate(tabId, pageId, address));
  }, [address, runNavigation, tabId]);

  const pointForEvent = useCallback((event: Pick<ReactPointerEvent<HTMLDivElement> | ReactWheelEvent<HTMLDivElement>, "clientX" | "clientY">) => {
    const canvas = canvasRef.current;
    const current = sessionRef.current;
    if (!canvas || !current) return null;
    return browserPointFromClient(event.clientX, event.clientY, canvas.getBoundingClientRect(), current);
  }, []);

  const focusKeyboard = useCallback(() => {
    keyboardRef.current?.focus({ preventScroll: true });
  }, []);

  const handlePointerDown = useCallback((event: ReactPointerEvent<HTMLDivElement>) => {
    const point = pointForEvent(event);
    const current = sessionRef.current;
    if (!point || !current) return;
    event.preventDefault();
    if (inspectingRef.current) {
      setInspectorError("");
      void app.BrowserInspectorSelect(tabId, current.pageId, point.x, point.y)
        .then((next) => {
          setSelection(next);
          setAnnotationText("");
          setInspectMode(false);
        })
        .catch((error) => setInspectorError(messageFrom(error)));
      return;
    }
    event.currentTarget.setPointerCapture(event.pointerId);
    focusKeyboard();
    void app.BrowserMouse(tabId, current.pageId, {
      type: "mousePressed",
      ...point,
      button: browserMouseButton(event.button),
      buttons: event.buttons,
      clickCount: event.detail || 1,
      modifiers: browserModifiers(event.nativeEvent),
    });
  }, [focusKeyboard, pointForEvent, setInspectMode, tabId]);

  const handlePointerUp = useCallback((event: ReactPointerEvent<HTMLDivElement>) => {
    const point = pointForEvent(event);
    const current = sessionRef.current;
    if (!point || !current) return;
    if (inspectingRef.current) {
      event.preventDefault();
      return;
    }
    event.preventDefault();
    if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId);
    void app.BrowserMouse(tabId, current.pageId, {
      type: "mouseReleased",
      ...point,
      button: browserMouseButton(event.button),
      buttons: event.buttons,
      clickCount: event.detail || 1,
      modifiers: browserModifiers(event.nativeEvent),
    });
  }, [pointForEvent, tabId]);

  const handlePointerMove = useCallback((event: ReactPointerEvent<HTMLDivElement>) => {
    const point = pointForEvent(event);
    if (!point) return;
    pendingPointerMoveRef.current = {
      ...point,
      buttons: event.buttons,
      modifiers: browserModifiers(event.nativeEvent),
    };
    if (inspectingRef.current) {
      hoverGenerationRef.current += 1;
      if (hoverTimerRef.current !== null) return;
      hoverTimerRef.current = window.setTimeout(() => {
        hoverTimerRef.current = null;
        const pending = pendingPointerMoveRef.current;
        const current = sessionRef.current;
        pendingPointerMoveRef.current = null;
        if (!pending || !current || !inspectingRef.current) return;
        const generation = hoverGenerationRef.current;
        void app.BrowserInspectorHover(tabId, current.pageId, pending.x, pending.y)
          .then((next) => {
            if (generation !== hoverGenerationRef.current || !inspectingRef.current) return;
            if (next.backendNodeId === hoveredNodeIdRef.current) return;
            hoveredNodeIdRef.current = next.backendNodeId;
            setHoveredElement(next);
            setInspectorError("");
          })
          .catch((error) => {
            if (generation === hoverGenerationRef.current) setInspectorError(messageFrom(error));
          });
      }, 45);
      return;
    }
    if (pointerMoveFrameRef.current !== null) return;
    pointerMoveFrameRef.current = window.requestAnimationFrame(() => {
      pointerMoveFrameRef.current = null;
      const pending = pendingPointerMoveRef.current;
      const current = sessionRef.current;
      pendingPointerMoveRef.current = null;
      if (!pending || !current) return;
      void app.BrowserMouse(tabId, current.pageId, {
        type: "mouseMoved",
        ...pending,
        button: "none",
      });
    });
  }, [pointForEvent, tabId]);

  const handleWheel = useCallback((event: ReactWheelEvent<HTMLDivElement>) => {
    const point = pointForEvent(event);
    const current = sessionRef.current;
    if (!point || !current) return;
    event.preventDefault();
    void app.BrowserMouse(tabId, current.pageId, {
      type: "mouseWheel",
      ...point,
      button: "none",
      deltaX: event.deltaX,
      deltaY: event.deltaY,
      modifiers: browserModifiers(event.nativeEvent),
    });
  }, [pointForEvent, tabId]);

  const handleKeyDown = useCallback((event: ReactKeyboardEvent<HTMLTextAreaElement>) => {
    const current = sessionRef.current;
    if (!current || event.nativeEvent.isComposing || composingRef.current) return;
    event.preventDefault();
    event.stopPropagation();
    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "v") return;
    void app.BrowserKey(tabId, current.pageId, browserKeyEvent(event.nativeEvent, "keyDown"));
  }, [tabId]);

  const handleKeyUp = useCallback((event: ReactKeyboardEvent<HTMLTextAreaElement>) => {
    const current = sessionRef.current;
    if (!current || event.nativeEvent.isComposing || composingRef.current) return;
    event.preventDefault();
    event.stopPropagation();
    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "v") return;
    void app.BrowserKey(tabId, current.pageId, browserKeyEvent(event.nativeEvent, "keyUp"));
  }, [tabId]);

  const handleCompositionStart = useCallback(() => {
    composingRef.current = true;
  }, []);

  const handleCompositionEnd = useCallback((event: CompositionEvent<HTMLTextAreaElement>) => {
    composingRef.current = false;
    event.currentTarget.value = "";
    const current = sessionRef.current;
    if (current && event.data) void app.BrowserInsertText(tabId, current.pageId, event.data);
  }, [tabId]);

  const applyInspectorStyles = useCallback(async (styles: Record<string, string>) => {
    const current = sessionRef.current;
    if (!current) return;
    const generation = ++styleApplyGenerationRef.current;
    setInspectorApplying(true);
    setInspectorError("");
    try {
      const next = await app.BrowserApplyStyles(tabId, current.pageId, styles);
      if (generation === styleApplyGenerationRef.current) setSelection(next);
    } catch (error) {
      if (generation === styleApplyGenerationRef.current) setInspectorError(messageFrom(error));
    } finally {
      if (generation === styleApplyGenerationRef.current) setInspectorApplying(false);
    }
  }, [tabId]);

  const clearInspector = useCallback(() => {
    const current = sessionRef.current;
    styleApplyGenerationRef.current += 1;
    setInspectMode(false);
    setSelection(null);
    setAnnotationText("");
    setInspectorApplying(false);
    setInspectorError("");
    if (current) void app.BrowserInspectorClear(tabId, current.pageId).catch((error) => setInspectorError(messageFrom(error)));
  }, [setInspectMode, tabId]);

  const toggleInspector = useCallback(() => {
    if (inspectingRef.current) {
      clearInspector();
      return;
    }
    if (selection) clearInspector();
    setInspectorError("");
    setInspectMode(true);
  }, [clearInspector, selection, setInspectMode]);

  const addAnnotationToConversation = useCallback(async (styles: Record<string, string>) => {
    const current = sessionRef.current;
    if (!selection || !current || !onInsertAnnotation) return;
    const generation = ++styleApplyGenerationRef.current;
    setInspectorApplying(true);
    setInspectorError("");
    try {
      const finalizedSelection = await app.BrowserApplyStyles(tabId, current.pageId, styles);
      if (generation !== styleApplyGenerationRef.current) return;
      setSelection(finalizedSelection);
      const annotation = createBrowserAnnotation(finalizedSelection, current, annotationText);
      onInsertAnnotation({
        text: formatBrowserAnnotation(annotation),
        insertSpacing: "block",
      });
    } catch (error) {
      if (generation === styleApplyGenerationRef.current) setInspectorError(messageFrom(error));
    } finally {
      if (generation === styleApplyGenerationRef.current) setInspectorApplying(false);
    }
  }, [annotationText, onInsertAnnotation, selection, tabId]);

  const activeElement = selection ?? (inspecting ? hoveredElement : null);
  const activeBox = useMemo(() => {
    if (!activeElement || !session) return null;
    return browserElementBoxToCanvas(activeElement.box, session, canvasSize);
  }, [activeElement, canvasSize, session]);
  const inspectorPosition = useMemo(() => {
    if (!selection || !activeBox) return null;
    return browserFloatingPosition(activeBox, canvasSize);
  }, [activeBox, canvasSize, selection]);

  const busy = status === "starting" || status === "loading";
  const unavailable = status === "unavailable" || status === "error" || status === "exited";

  return (
    <section className="browser-panel" aria-label={t("browser.title")}>
      <form className="browser-panel__toolbar" onSubmit={navigate}>
        <button
          type="button"
          className="browser-panel__action"
          disabled={!session?.canGoBack || busy}
          onClick={() => session && void runNavigation(() => app.BrowserGoBack(tabId, session.pageId))}
          aria-label={t("browser.back")}
          title={t("browser.back")}
        >
          <ArrowLeft size={15} aria-hidden="true" />
        </button>
        <button
          type="button"
          className="browser-panel__action"
          disabled={!session?.canGoForward || busy}
          onClick={() => session && void runNavigation(() => app.BrowserGoForward(tabId, session.pageId))}
          aria-label={t("browser.forward")}
          title={t("browser.forward")}
        >
          <ArrowRight size={15} aria-hidden="true" />
        </button>
        <button
          type="button"
          className="browser-panel__action"
          disabled={!session || busy}
          onClick={() => session && void runNavigation(() => app.BrowserReload(tabId, session.pageId))}
          aria-label={t("browser.reload")}
          title={t("browser.reload")}
        >
          <RefreshCw size={14} aria-hidden="true" />
        </button>
        <button
          type="button"
          className={`browser-panel__action${inspecting ? " browser-panel__action--active" : ""}`}
          disabled={!session || busy || !session.canAnnotate}
          onClick={toggleInspector}
          aria-pressed={inspecting}
          aria-label={session && !session.canAnnotate ? t("browser.localAnnotationOnly") : t("browser.selectElement")}
          title={session && !session.canAnnotate ? t("browser.localAnnotationOnly") : t("browser.selectElement")}
        >
          <Crosshair size={14} aria-hidden="true" />
        </button>
        <div className="browser-panel__address-wrap">
          <Globe2 size={13} aria-hidden="true" />
          <input
            className="browser-panel__address"
            value={address}
            disabled={!session}
            placeholder={t("browser.addressPlaceholder")}
            aria-label={t("browser.address")}
            spellCheck={false}
            onFocus={(event) => {
              addressEditingRef.current = true;
              event.currentTarget.select();
            }}
            onBlur={() => { addressEditingRef.current = false; }}
            onChange={(event) => setAddress(event.target.value)}
          />
        </div>
      </form>

      <div className="browser-panel__body">
        <div
          ref={canvasRef}
          className={`browser-panel__canvas${inspecting ? " browser-panel__canvas--inspecting" : ""}`}
          tabIndex={-1}
          onPointerDown={handlePointerDown}
          onPointerUp={handlePointerUp}
          onPointerMove={handlePointerMove}
          onWheel={handleWheel}
          onContextMenu={(event) => event.preventDefault()}
        >
          {frame ? (
            <img
              className="browser-panel__frame"
              src={`data:image/jpeg;base64,${frame.data}`}
              alt={session?.title || session?.url || t("browser.page")}
              draggable={false}
              onLoad={() => {
                if (frame.sequence !== frameSequenceRef.current) return;
                void app.BrowserFrameAck(frame.tabId, frame.pageId, frame.sequence);
              }}
            />
          ) : (
            <div className="browser-panel__blank" aria-hidden={unavailable ? undefined : "true"}>
              {busy ? <LoaderCircle className="browser-panel__spinner" size={20} /> : <Globe2 size={28} />}
            </div>
          )}
          <textarea
            ref={keyboardRef}
            className="browser-panel__keyboard"
            aria-label={t("browser.keyboardInput")}
            onKeyDown={handleKeyDown}
            onKeyUp={handleKeyUp}
            onCompositionStart={handleCompositionStart}
            onCompositionEnd={handleCompositionEnd}
            onPaste={(event) => {
              event.preventDefault();
              const current = sessionRef.current;
              const text = event.clipboardData.getData("text/plain");
              if (current && text) void app.BrowserInsertText(tabId, current.pageId, text);
            }}
          />
          {inspecting && (
            <div className="browser-panel__inspect-hint" role="status">
              <Crosshair size={13} aria-hidden="true" />
              {t("browser.selectElementHint")}
            </div>
          )}
          {activeElement && activeBox && (
            <div
              className={`browser-element-highlight${selection ? " browser-element-highlight--locked" : ""}`}
              style={{
                left: activeBox.x,
                top: activeBox.y,
                width: activeBox.width,
                height: activeBox.height,
              }}
              aria-hidden="true"
            >
              <div className={`browser-element-label${activeBox.y < 30 ? " browser-element-label--inside" : ""}`}>
                <strong>{activeElement.tag}</strong>
                <span>{Math.round(activeElement.box.width)} × {Math.round(activeElement.box.height)}</span>
                {activeElement.computedStyles["font-size"] && <span>{activeElement.computedStyles["font-size"]}</span>}
                {activeElement.computedStyles["font-family"] && <span>{activeElement.computedStyles["font-family"]}</span>}
              </div>
            </div>
          )}
          {selection && inspectorPosition && (
            <BrowserStyleInspector
              selection={selection}
              position={inspectorPosition}
              annotationText={annotationText}
              applying={inspectorApplying}
              error={inspectorError}
              onAnnotationTextChange={setAnnotationText}
              onApply={applyInspectorStyles}
              onClose={clearInspector}
              onAddToConversation={addAnnotationToConversation}
            />
          )}
          {unavailable && (
            <div className="browser-panel__overlay" role="status">
              <AlertCircle size={20} aria-hidden="true" />
              <strong>{status === "unavailable" ? t("browser.unavailable") : t("browser.failed")}</strong>
              {detail && <span>{detail}</span>}
              {status !== "unavailable" && (
                <button type="button" onClick={() => void start()}>{t("browser.retry")}</button>
              )}
            </div>
          )}
        </div>
      </div>
    </section>
  );
}