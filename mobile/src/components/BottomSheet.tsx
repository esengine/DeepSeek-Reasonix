import { X } from "lucide-react";
import {
  useEffect,
  useId,
  useState,
  type ReactNode,
} from "react";

/**
 * Animated bottom sheet with enter/exit (120/180/260ms tokens).
 * Keeps children mounted until exit animation completes.
 */
export function BottomSheet({
  open,
  title,
  description,
  localeCloseLabel,
  onClose,
  children,
  wide = false,
}: {
  open: boolean;
  title: string;
  description?: string;
  localeCloseLabel: string;
  onClose: () => void;
  children: ReactNode;
  /** Taller panel for approval content */
  wide?: boolean;
}) {
  const titleId = useId();
  const [render, setRender] = useState(open);
  const [phase, setPhase] = useState<"in" | "out">("in");

  useEffect(() => {
    if (open) {
      setRender(true);
      setPhase("in");
      return;
    }
    if (!render) return;
    setPhase("out");
    const t = window.setTimeout(() => setRender(false), 260);
    return () => window.clearTimeout(t);
  }, [open, render]);

  useEffect(() => {
    if (!render) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [render, onClose]);

  useEffect(() => {
    if (!render) return;
    const prev = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.body.style.overflow = prev;
    };
  }, [render]);

  if (!render) return null;

  return (
    <div
      className="sheet-root"
      data-phase={phase}
      role="dialog"
      aria-modal="true"
      aria-labelledby={titleId}
    >
      <button
        type="button"
        className="sheet-backdrop"
        aria-label={localeCloseLabel}
        onClick={onClose}
      />
      <div className={`sheet-panel${wide ? " sheet-panel-wide" : ""}`} data-phase={phase}>
        <div className="sheet-handle" aria-hidden />
        <div className="sheet-header">
          <div className="sheet-header-text">
            <h2 id={titleId} className="sheet-title">
              {title}
            </h2>
            {description ? <p className="sheet-desc">{description}</p> : null}
          </div>
          <button
            type="button"
            className="icon-btn neutral"
            aria-label={localeCloseLabel}
            onClick={onClose}
          >
            <X size={20} />
          </button>
        </div>
        {children}
      </div>
    </div>
  );
}
