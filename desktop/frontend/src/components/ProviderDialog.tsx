import { useEffect, useId, useLayoutEffect, useRef, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useT } from "../lib/i18n";
import { ModalCloseButton } from "./ModalCloseButton";

/** A settings child dialog that owns focus and Escape without closing settings. */
export function ProviderDialog({ title, children, onClose }: { title: string; children: ReactNode; onClose: () => void }) {
  const id = useId();
  const ref = useRef<HTMLDivElement>(null);
  const closeRef = useRef(onClose);
  closeRef.current = onClose;
  const t = useT();
  useLayoutEffect(() => {
    const previous = document.activeElement as HTMLElement | null;
    ref.current?.querySelector<HTMLElement>("input:not(:disabled), select:not(:disabled)")?.focus();
    return () => { if (previous?.isConnected) previous.focus(); };
  }, []);
  useEffect(() => {
    const keydown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault(); event.stopImmediatePropagation(); closeRef.current();
      }
      if (event.key === "Tab") {
        const elements = Array.from(ref.current?.querySelectorAll<HTMLElement>("button:not(:disabled), input:not(:disabled), select:not(:disabled), textarea:not(:disabled), summary") ?? []);
        const first = elements[0], last = elements[elements.length - 1];
        if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus(); }
        else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); }
      }
    };
    window.addEventListener("keydown", keydown, true);
    return () => window.removeEventListener("keydown", keydown, true);
  }, []);
  return createPortal(<div className="modal-backdrop provider-dialog-backdrop" onMouseDown={(e) => { if (e.target === e.currentTarget) onClose(); }}>
    <div ref={ref} className="modal provider-dialog" role="dialog" aria-modal="true" aria-labelledby={id}>
      <header><h3 id={id}>{title}</h3><ModalCloseButton label={t("common.close")} onClick={onClose} /></header>
      {children}
    </div>
  </div>, document.body);
}
