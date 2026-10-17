import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from "react";
import { ErrorMessage } from "../components/ErrorMessage";
import { useT } from "./i18n";

export interface Toast {
  id: number;
  text: string;
  level: "info" | "warn" | "error";
  summary?: string;
  actionLabel?: string;
  onAction?: () => void;
}

export interface ToastOptions {
  summary?: string;
  actionLabel?: string;
  onAction?: () => void;
  durationMs?: number;
}

export interface ToastContextValue {
  toasts: Toast[];
  showToast: (text: string, level?: Toast["level"], options?: ToastOptions) => void;
}

const ToastContext = createContext<ToastContextValue>({ toasts: [], showToast: () => {} });

export function useToast() {
  return useContext(ToastContext);
}

let nextId = 1;

export function ToastProvider({ children }: { children: ReactNode }) {
  const translate = useT();
  const [toasts, setToasts] = useState<Toast[]>([]);
  const timers = useRef(new Map<number, ReturnType<typeof setTimeout>>());
  useEffect(() => {
    const activeTimers = timers.current;
    return () => { for (const timer of activeTimers.values()) clearTimeout(timer); activeTimers.clear(); };
  }, []);

  const showToast = useCallback((text: string, level: Toast["level"] = "info", options: ToastOptions = {}) => {
    const id = nextId++;
    setToasts((prev) => [...prev, { id, text, level, summary: options.summary, actionLabel: options.actionLabel, onAction: options.onAction }]);
    const timer = setTimeout(() => {
      setToasts((prev) => prev.filter((t) => t.id !== id));
      timers.current.delete(id);
    }, options.durationMs ?? (options.actionLabel || level !== "info" ? 8000 : 2500));
    timers.current.set(id, timer);
  }, []);

  const dismissToast = useCallback((id: number) => {
    const timer = timers.current.get(id);
    if (timer) clearTimeout(timer);
    timers.current.delete(id);
    setToasts((prev) => prev.filter((t) => t.id !== id));
  }, []);

  return (
    <ToastContext.Provider value={{ toasts, showToast }}>
      {children}
      <div className="toast-container" role="status" aria-live="polite">
        {toasts.map((t) => (
          <div key={t.id} className={`toast toast--${t.level}`} onClick={() => dismissToast(t.id)}>
            {t.level === "warn" && <span className="toast__icon">⚠️</span>}
            {t.level === "error" && <span className="toast__icon">❌</span>}
            <span className="toast__text">{t.level === "info" ? t.text : <ErrorMessage error={t.text} summary={t.summary} onInspect={() => {
              clearTimeout(timers.current.get(t.id));
              timers.current.delete(t.id);
            }} />}</span>
            {t.actionLabel && t.onAction && (
              <button
                type="button"
                className="toast__action"
                onClick={(event) => {
                  event.stopPropagation();
                  dismissToast(t.id);
                  t.onAction?.();
                }}
              >
                {t.actionLabel}
              </button>
            )}
            <button type="button" className="toast__action" aria-label={translate("error.dismiss")}
              onClick={(event) => { event.stopPropagation(); dismissToast(t.id); }}>×</button>
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  );
}
