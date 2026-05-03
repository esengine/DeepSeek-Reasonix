import { createContext, useContext, useEffect } from "react";
import type { Keystroke } from "./keystroke.js";
import type { KeystrokeListener, KeystrokeReader } from "./reader.js";

export const KeystrokeContext = createContext<KeystrokeReader | null>(null);

export function useKeystroke(handler: (key: Keystroke) => void, enabled = true): void {
  const reader = useContext(KeystrokeContext);
  useEffect(() => {
    if (!enabled || !reader) return;
    const wrapped: KeystrokeListener = (key) => handler(key);
    return reader.subscribe(wrapped);
  }, [reader, handler, enabled]);
}
