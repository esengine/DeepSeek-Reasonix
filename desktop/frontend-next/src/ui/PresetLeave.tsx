import { useCallback, useEffect, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { t } from "../i18n";
import { listenAction } from "./listen";
import { useModalFocus } from "./useModalFocus";
import { StudioIcon } from "./StudioIcon";
import { useSettingsLeaveGuard } from "./SettingsLeave";

export function usePresetLeave(dirty: boolean, busy: boolean, changes: string[] = []) {
  const [asked, setAsked] = useState<{ leave: () => void; from: HTMLElement | null } | null>(null);
  const request = useCallback((leave: () => void) => {
    if (busy) return;
    if (!dirty) { leave(); return; }
    const from = document.activeElement;
    setAsked({ leave, from: from instanceof HTMLElement ? from : null });
  }, [dirty, busy]);
  useSettingsLeaveGuard(request);
  useEffect(() => {
    if (!dirty) return;
    const beforeUnload = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      event.returnValue = "";
    };
    window.addEventListener("beforeunload", beforeUnload);
    return () => window.removeEventListener("beforeunload", beforeUnload);
  }, [dirty]);
  const keep = () => {
    const from = asked?.from;
    setAsked(null);
    if (from?.isConnected) from.focus();
  };
  const confirmation = asked && <PresetLeaveDialog changes={changes} onKeep={keep} onDiscard={() => {
    const leave = asked.leave;
    setAsked(null);
    leave();
  }} />;
  return { request, confirmation };
}

function PresetLeaveDialog({ changes, onKeep, onDiscard }: { changes: string[]; onKeep: () => void; onDiscard: () => void }) {
  const root = useRef<HTMLDivElement>(null);
  const title = useId();
  useModalFocus(root);
  useEffect(() => {
    root.current?.querySelector<HTMLButtonElement>("[data-primary]")?.focus();
    return listenAction(window, "keydown", { action: "agent-preset.leave-cancel", listener: (event) => {
      if ((event as KeyboardEvent).key !== "Escape") return;
      event.preventDefault();
      event.stopImmediatePropagation();
      onKeep();
    } }, true);
  }, [onKeep]);
  return createPortal(<div className="preset-confirm-veil">
    <div className="preset-confirm" ref={root} role="alertdialog" aria-modal="true" aria-labelledby={title}>
      <header>
        <h3 id={title}>{t("persona.discard_unsaved_changes")}</h3>
        <button type="button" className="pc-x" data-action="agent-preset.leave-cancel" aria-label={t("common.close")} title={t("common.close")} onClick={onKeep}><StudioIcon name="close" /></button>
      </header>
      <div className="pc-body">
        <p>{t("common.they_will_be_lost_if")}</p>
        {changes.length > 0 && <div className="pc-chips">{changes.map((c) => <span key={c}>{c}</span>)}</div>}
      </div>
      <div className="preset-confirm-actions">
        <button className="btn" data-danger="" data-action="agent-preset.leave-discard" onClick={onDiscard}>{t("persona.discard_changes")}</button>
        <button className="btn" data-primary="" data-action="agent-preset.leave-cancel" onClick={onKeep}>{t("persona.keep_editing")}</button>
      </div>
    </div>
  </div>, document.body);
}
