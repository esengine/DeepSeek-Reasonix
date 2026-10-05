import { useCallback, useRef } from "react";

// A Chinese or Japanese IME confirms its candidate with Enter, and several of
// them end the composition just before that keydown arrives — so isComposing
// alone is already false and the confirm reads as "send". This window covers
// the reordering; it stays short so a deliberate second Enter still lands.
const GRACE_MS = 100;

// useIme reports whether a key event belongs to an input method rather than to
// the composer. Every key the composer acts on has to ask, or picking a word
// sends a half-written message.
export function useIme() {
  const composing = useRef(false);
  const endedAt = useRef(0);

  const isIme = useCallback((e: { isComposing?: boolean; keyCode?: number }) => {
    if (composing.current || e.isComposing === true || e.keyCode === 229) return true;
    return endedAt.current > 0 && Date.now() - endedAt.current < GRACE_MS;
  }, []);

  return {
    isIme,
    handlers: {
      onCompositionStart: () => {
        composing.current = true;
      },
      onCompositionEnd: () => {
        composing.current = false;
        endedAt.current = Date.now();
      },
    },
  };
}
