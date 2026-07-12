import { MessageSquarePlus, RotateCcw, Undo2, X } from "lucide-react";
import { useEffect, useMemo, useRef, useState, type PointerEvent, type WheelEvent } from "react";

import type { BrowserElementView } from "../lib/bridge";
import { stepBrowserCSSNumericValue, type BrowserFloatingPosition } from "../lib/browserInput";
import { useT } from "../lib/i18n";

const STYLE_FIELDS = [
  "color",
  "background-color",
  "opacity",
  "font-family",
  "font-size",
  "font-weight",
  "line-height",
  "margin",
  "padding",
  "gap",
  "width",
  "height",
  "border",
  "border-radius",
  "display",
  "flex-direction",
  "align-items",
  "justify-content",
  "grid-template-columns",
] as const;

interface BrowserStyleInspectorProps {
  selection: BrowserElementView;
  position: BrowserFloatingPosition;
  annotationText: string;
  applying: boolean;
  error?: string;
  onAnnotationTextChange: (value: string) => void;
  onApply: (styles: Record<string, string>) => Promise<void>;
  onClose: () => void;
  onAddToConversation: (styles: Record<string, string>) => Promise<void>;
}

export function BrowserStyleInspector({
  selection,
  position,
  annotationText,
  applying,
  error,
  onAnnotationTextChange,
  onApply,
  onClose,
  onAddToConversation,
}: BrowserStyleInspectorProps) {
  const t = useT();
  const applyTimerRef = useRef<number | null>(null);
  const [draft, setDraft] = useState<Record<string, string>>(selection.styleOverrides ?? {});

  useEffect(() => {
    if (applyTimerRef.current !== null) window.clearTimeout(applyTimerRef.current);
    setDraft(selection.styleOverrides ?? {});
    return () => {
      if (applyTimerRef.current !== null) window.clearTimeout(applyTimerRef.current);
    };
  }, [selection.backendNodeId, selection.selector, selection.styleOverrides]);

  const elementLabel = useMemo(
    () => selection.accessibleName || selection.text || selection.selector,
    [selection.accessibleName, selection.selector, selection.text],
  );

  const commit = (next: Record<string, string>) => {
    setDraft(next);
    if (applyTimerRef.current !== null) window.clearTimeout(applyTimerRef.current);
    applyTimerRef.current = window.setTimeout(() => {
      applyTimerRef.current = null;
      void onApply(next);
    }, 180);
  };

  const updateStyle = (name: string, value: string) => {
    const next = { ...draft };
    const normalized = value.trim();
    if (normalized) next[name] = value;
    else delete next[name];
    commit(next);
  };

  const resetStyle = (name: string) => {
    const next = { ...draft };
    delete next[name];
    commit(next);
  };

  const stepStyle = (name: string, rawValue: string, direction: 1 | -1, accelerated: boolean) => {
    const source = rawValue.trim() || selection.computedStyles[name] || "";
    const next = stepBrowserCSSNumericValue(name, source, direction, accelerated);
    if (next !== null) updateStyle(name, next);
    return next !== null;
  };

  const addToConversation = () => {
    if (applyTimerRef.current !== null) {
      window.clearTimeout(applyTimerRef.current);
      applyTimerRef.current = null;
    }
    void onAddToConversation(draft);
  };

  const stopPointerEvent = (event: PointerEvent<HTMLElement>) => event.stopPropagation();
  const stopWheelEvent = (event: WheelEvent<HTMLElement>) => event.stopPropagation();

  return (
    <aside
      className="browser-inspector"
      style={{ left: position.x, top: position.y, width: position.width, maxHeight: position.maxHeight }}
      aria-label={t("browser.inspector")}
      onPointerDown={stopPointerEvent}
      onPointerUp={stopPointerEvent}
      onPointerMove={stopPointerEvent}
      onWheel={stopWheelEvent}
      onContextMenu={(event) => event.stopPropagation()}
    >
      <header className="browser-inspector__header">
        <div className="browser-inspector__identity">
          <strong>{selection.tag}</strong>
          <span title={elementLabel}>{elementLabel}</span>
        </div>
        <button type="button" className="browser-inspector__icon" onClick={onClose} aria-label={t("browser.inspectorClose")}>
          <X size={14} aria-hidden="true" />
        </button>
      </header>

      <div className="browser-inspector__annotation">
        <label htmlFor={`browser-annotation-${selection.backendNodeId}`}>{t("browser.annotationLabel")}</label>
        <textarea
          id={`browser-annotation-${selection.backendNodeId}`}
          value={annotationText}
          placeholder={t("browser.annotationPlaceholder")}
          rows={3}
          onChange={(event) => onAnnotationTextChange(event.target.value)}
        />
      </div>

      <div className="browser-inspector__meta">
        <div className="browser-inspector__element-meta">
          <code title={selection.selector}>{selection.selector}</code>
          <span>{Math.round(selection.box.width)} × {Math.round(selection.box.height)}</span>
        </div>
        <div className="browser-inspector__computed">
          <span><small>{t("browser.computedColor")}</small>{selection.computedStyles.color || "—"}</span>
          <span><small>{t("browser.computedFont")}</small>{selection.computedStyles["font-family"] || "—"}</span>
          <span><small>{t("browser.computedSize")}</small>{selection.computedStyles["font-size"] || "—"}</span>
          <span><small>{t("browser.computedWeight")}</small>{selection.computedStyles["font-weight"] || "—"}</span>
          <span><small>{t("browser.computedLineHeight")}</small>{selection.computedStyles["line-height"] || "—"}</span>
        </div>
      </div>

      <div className="browser-inspector__styles">
        <div className="browser-inspector__section-title">{t("browser.styleSection")}</div>
        {STYLE_FIELDS.map((name) => {
          const overridden = Object.prototype.hasOwnProperty.call(draft, name);
          return (
            <label className="browser-inspector__field" key={name}>
              <span>{name}</span>
              <div className="browser-inspector__input-row">
                <input
                  value={draft[name] ?? ""}
                  placeholder={selection.computedStyles[name] || t("browser.styleUnset")}
                  spellCheck={false}
                  onChange={(event) => updateStyle(name, event.target.value)}
                  onKeyDown={(event) => {
                    if (event.key !== "ArrowUp" && event.key !== "ArrowDown") return;
                    const changed = stepStyle(
                      name,
                      event.currentTarget.value,
                      event.key === "ArrowUp" ? 1 : -1,
                      event.shiftKey,
                    );
                    if (changed) event.preventDefault();
                  }}
                />
                <button
                  type="button"
                  className="browser-inspector__undo"
                  disabled={!overridden}
                  onClick={() => resetStyle(name)}
                  aria-label={`${t("browser.styleReset")} ${name}`}
                  title={t("browser.styleReset")}
                >
                  <Undo2 size={12} aria-hidden="true" />
                </button>
              </div>
            </label>
          );
        })}
      </div>

      {error && <div className="browser-inspector__error" role="alert">{error}</div>}

      <footer className="browser-inspector__footer">
        <button type="button" className="browser-inspector__secondary" disabled={applying || Object.keys(draft).length === 0} onClick={() => commit({})}>
          <RotateCcw size={13} aria-hidden="true" />
          {t("browser.styleClear")}
        </button>
        <button type="button" className="browser-inspector__primary" disabled={applying} onClick={addToConversation}>
          <MessageSquarePlus size={13} aria-hidden="true" />
          {t("browser.addToConversation")}
        </button>
      </footer>
    </aside>
  );
}