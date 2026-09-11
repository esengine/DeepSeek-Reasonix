import { useEffect, useLayoutEffect, useRef, type RefObject } from "react";

// Plain-textarea IME guard (#8593/#8409). While a composition is active the
// composer textarea renders uncontrolled (value={undefined}), so no unrelated
// re-render (autosize, selection tracking, run-strip ticker) can write
// node.value and cancel the in-flight composition — React 19's updateTextarea
// compares the controlled value against the live DOM value on every commit,
// which is what swallowed the first CJK keystroke. onChange still flows into
// state; compositionend (or a non-composing commit input) ends the freeze and
// the next render resyncs against the live DOM value without clobbering it.

export interface ComposerImeGuardOptions {
  taRef: RefObject<HTMLTextAreaElement | null>;
  text: string;
  // Drives listener (re)attachment: the plain textarea unmounts while an
  // invocation token mounts the rich input.
  invocationCount: number;
  textRef: RefObject<string>;
  lastSelectionRef: RefObject<{ start: number; end: number }>;
  setText: (next: string) => void;
  setPlainSelection: (selection: { start: number; end: number }) => void;
}

export interface ComposerImeGuard {
  composingRef: RefObject<boolean>;
  lastCompositionEndAt: RefObject<number>;
  // Feeds the textarea's onChange into the freeze bookkeeping.
  trackImeInputChange: (nativeEvent: InputEvent, inputType: string | undefined, value: string) => void;
}

export function useComposerImeGuard(options: ComposerImeGuardOptions): ComposerImeGuard {
  const { taRef, text, invocationCount, textRef, lastSelectionRef, setText, setPlainSelection } = options;
  const composingRef = useRef(false);
  const lastCompositionEndAt = useRef(0);
  // Latest text the IME path itself produced, so a programmatic setText
  // landing mid-composition can be told apart from IME input and force a
  // resync.
  const imeStateTextRef = useRef<string | null>(null);

  // Native composition listeners, the same mechanism the rich input uses:
  // React's synthetic composition events fall back to keyCode-229 inference
  // wherever CompositionEvent is missing, and the freeze bookkeeping must run
  // synchronously with the browser's composition lifecycle.
  useEffect(() => {
    const node = taRef.current;
    if (!node) return;
    const onStart = () => {
      composingRef.current = true;
      imeStateTextRef.current = textRef.current;
    };
    const onEnd = () => {
      composingRef.current = false;
      lastCompositionEndAt.current = Date.now();
      imeStateTextRef.current = null;
      // compositionend's DOM value is authoritative: an IME cancel restores
      // the pre-composition text while state may still hold the provisional
      // text.
      if (node.value !== textRef.current) {
        const nextSelection = {
          start: node.selectionStart ?? node.value.length,
          end: node.selectionEnd ?? node.value.length,
        };
        textRef.current = node.value;
        setText(node.value);
        lastSelectionRef.current = nextSelection;
        setPlainSelection(nextSelection);
      }
    };
    node.addEventListener("compositionstart", onStart);
    node.addEventListener("compositionend", onEnd);
    return () => {
      node.removeEventListener("compositionstart", onStart);
      node.removeEventListener("compositionend", onEnd);
      // Unmounting the textarea mid-composition (e.g. an invocation token
      // swaps in the rich input) may never deliver compositionend; leaving
      // composingRef stuck would suppress Enter-to-send forever.
      if (composingRef.current) {
        composingRef.current = false;
        lastCompositionEndAt.current = Date.now();
        imeStateTextRef.current = null;
      }
    };
  }, [invocationCount, taRef, textRef, lastSelectionRef, setText, setPlainSelection]);

  // Programmatic setText (history recall, menu inserts, draft switches)
  // bypasses the textarea's onChange, so while the IME freeze renders the
  // textarea uncontrolled those writes would never reach the DOM. A text
  // change the IME path did not produce forces an authoritative resync and
  // ends the frozen composition: programmatic content wins.
  useLayoutEffect(() => {
    if (!composingRef.current) return;
    if (imeStateTextRef.current === text) return;
    composingRef.current = false;
    imeStateTextRef.current = null;
    const node = taRef.current;
    if (!node) return;
    node.value = text;
    const caret = Math.min(lastSelectionRef.current.start, text.length);
    try {
      node.setSelectionRange(caret, caret);
    } catch {
      // Detached/jsdom node: caret restore is best-effort.
    }
  }, [text, taRef, lastSelectionRef]);

  const trackImeInputChange = (nativeEvent: InputEvent, inputType: string | undefined, value: string) => {
    if (!composingRef.current) return;
    if (nativeEvent.isComposing || inputType === "insertCompositionText") {
      // Mid-composition edits still reach state so app logic (menus,
      // counters, drafts) sees the live text; the uncontrolled render keeps
      // the provisional text out of React's DOM sync.
      imeStateTextRef.current = value;
      return;
    }
    // A non-composition input while frozen is the commit (Chromium can fire
    // it before compositionend; WebView2 can deliver the committed text in a
    // following non-composing input): end the freeze so this render resyncs
    // normally.
    composingRef.current = false;
    imeStateTextRef.current = null;
  };

  return { composingRef, lastCompositionEndAt, trackImeInputChange };
}
