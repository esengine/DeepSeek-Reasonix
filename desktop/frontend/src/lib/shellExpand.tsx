import { createContext, useCallback, useContext, useMemo, useRef, type ReactNode } from "react";

// Shell-expand coordination: ToolCards register their toggle callbacks, and
// Cmd+B (from App) calls the most recent one.

type ToggleFn = () => void;

interface ShellExpandCtx {
  register: (id: string, toggle: ToggleFn) => void;
  toggleLast: () => void;
}

const Ctx = createContext<ShellExpandCtx | null>(null);

export function ShellExpandProvider({ children }: { children: ReactNode }) {
  const mapRef = useRef(new Map<string, ToggleFn>());
  const orderRef: { current: string[] } = useRef<string[]>([]);

  const register = useCallback((id: string, toggle: ToggleFn) => {
    mapRef.current.set(id, toggle);
    if (!orderRef.current.includes(id)) {
      orderRef.current.push(id);
    }
    return () => {
      mapRef.current.delete(id);
      orderRef.current = orderRef.current.filter((x) => x !== id);
    };
  }, []);

  const toggleLast = useCallback(() => {
    const ids = orderRef.current;
    if (ids.length === 0) return;
    const fn = mapRef.current.get(ids[ids.length - 1]);
    fn?.();
  }, []);

  const value = useMemo(() => ({ register, toggleLast }), [register, toggleLast]);
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useShellExpand() {
  return useContext(Ctx);
}
