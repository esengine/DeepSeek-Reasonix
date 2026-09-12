import { useEffect, useId, useLayoutEffect, useRef } from "react";
import { CircleAlert, X } from "lucide-react";
import { createPortal } from "react-dom";

export type RiskConfirmationProps = {
  open: boolean;
  title: string;
  description: string;
  acknowledgeLabel: string;
  cancelLabel: string;
  closeLabel: string;
  confirmLabel: string;
  acknowledged: boolean;
  disabled?: boolean;
  onAcknowledgedChange: (acknowledged: boolean) => void;
  onCancel: () => void;
  onConfirm: () => void;
};

/** Explicit acknowledgement gate for sensitive choices such as Full access. */
export function RiskConfirmation({
  open,
  title,
  description,
  acknowledgeLabel,
  cancelLabel,
  closeLabel,
  confirmLabel,
  acknowledged,
  disabled = false,
  onAcknowledgedChange,
  onCancel,
  onConfirm,
}: RiskConfirmationProps) {
  const titleId = useId();
  const descriptionId = useId();
  const checkboxRef = useRef<HTMLInputElement>(null);
  const cancelRef = useRef<HTMLButtonElement>(null);
  const confirmRef = useRef<HTMLButtonElement>(null);
  const closeRef = useRef<HTMLButtonElement>(null);
  const restoreFocusRef = useRef<HTMLElement | null>(null);

  useLayoutEffect(() => {
    if (!open) return;
    const activeElement = document.activeElement;
    restoreFocusRef.current = activeElement && "focus" in activeElement ? activeElement as HTMLElement : null;
    checkboxRef.current?.focus();
    return () => {
      if (restoreFocusRef.current?.isConnected) restoreFocusRef.current.focus();
      restoreFocusRef.current = null;
    };
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        event.stopPropagation();
        onCancel();
        return;
      }
      if (event.key !== "Tab") return;
      const focusable = [closeRef.current, checkboxRef.current, cancelRef.current, confirmRef.current]
        .filter((node): node is HTMLButtonElement | HTMLInputElement => node !== null && !node.disabled);
      if (focusable.length === 0) return;
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      if (!first || !last) return;
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    };
    document.addEventListener("keydown", onKeyDown, { capture: true });
    return () => document.removeEventListener("keydown", onKeyDown, { capture: true });
  }, [onCancel, open]);

  if (!open) return null;

  return createPortal(
    <div className="risk-confirmation" data-app-overlay="" role="presentation">
      <div className="risk-confirmation__mask" aria-hidden="true" onMouseDown={onCancel} />
      <section
        className="risk-confirmation__dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={descriptionId}
      >
        <div className="risk-confirmation__content">
          <header className="risk-confirmation__header">
            <h2 id={titleId}>{title}</h2>
            <button ref={closeRef} type="button" className="risk-confirmation__close" aria-label={closeLabel} disabled={disabled} onClick={onCancel}>
              <X size={18} strokeWidth={1.8} aria-hidden="true" />
            </button>
          </header>
          <div className="risk-confirmation__body">
            <div className="risk-confirmation__warning">
              <CircleAlert size={18} strokeWidth={2} aria-hidden="true" />
              <p id={descriptionId}>{description}</p>
            </div>
            <label className="risk-confirmation__acknowledgement">
              <input
                ref={checkboxRef}
                type="checkbox"
                checked={acknowledged}
                disabled={disabled}
                onChange={(event) => onAcknowledgedChange(event.currentTarget.checked)}
              />
              <span>{acknowledgeLabel}</span>
            </label>
          </div>
        </div>
        <footer className="risk-confirmation__footer">
          <button ref={cancelRef} type="button" className="btn risk-confirmation__cancel" disabled={disabled} onClick={onCancel}>{cancelLabel}</button>
          <button
            ref={confirmRef}
            type="button"
            className="btn btn--primary risk-confirmation__confirm"
            disabled={disabled || !acknowledged}
            onClick={onConfirm}
          >
            {confirmLabel}
          </button>
        </footer>
      </section>
    </div>,
    document.body,
  );
}
