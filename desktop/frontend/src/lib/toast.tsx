import { createContext, useCallback, useContext, useRef, useState, type ReactNode } from "react";

export interface Toast {
  id: number;
  text: string;
  level: "info" | "warn" | "error";
  actionLabel?: string;
  onAction?: () => void;
}

export interface ToastOptions {
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
  const [toasts, setToasts] = useState<Toast[]>([]);
  const timers = useRef(new Map<number, ReturnType<typeof setTimeout>>());

  const showToast = useCallback((text: string, level: Toast["level"] = "info", options: ToastOptions = {}) => {
    const id = nextId++;
    setToasts((prev) => [...prev, { id, text, level, actionLabel: options.actionLabel, onAction: options.onAction }]);
    const timer = setTimeout(() => {
      setToasts((prev) => prev.filter((t) => t.id !== id));
      timers.current.delete(id);
    }, options.durationMs ?? (options.actionLabel ? 8000 : 2500));
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
            <span className="toast__text">{t.text}</span>
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
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  );
}
