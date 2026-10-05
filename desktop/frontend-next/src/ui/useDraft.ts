import { useEffect, useRef, useState } from "react";
import { readDraft, writeDraft } from "./drafts";

export function useDraft(draftKey: string, submitting: boolean) {
  const [text, setText] = useState(() => readDraft(draftKey));
  const keyRef = useRef(draftKey);
  const textRef = useRef(text);
  const pendingRef = useRef<{ text: string; key: string } | null>(null);
  const skipWrite = useRef(false);
  textRef.current = text;

  useEffect(() => {
    if (keyRef.current === draftKey) return;
    const previous = keyRef.current;
    writeDraft(previous, pendingRef.current?.text ?? textRef.current);
    keyRef.current = draftKey;
    if (!draftKey) return;
    // A newly known session key must not replay the draft being submitted.
    if (pendingRef.current) {
      writeDraft(draftKey, pendingRef.current.text);
      skipWrite.current = true;
      return;
    }
    const saved = readDraft(draftKey);
    if (!previous && !saved && textRef.current) writeDraft(draftKey, textRef.current);
    else setText(saved);
    skipWrite.current = true;
  }, [draftKey]);

  useEffect(() => {
    if (!draftKey || skipWrite.current || submitting) {
      skipWrite.current = false;
      return;
    }
    const timer = window.setTimeout(() => writeDraft(draftKey, text), 250);
    return () => window.clearTimeout(timer);
  }, [draftKey, text, submitting]);

  useEffect(() => {
    const flush = () => writeDraft(keyRef.current, pendingRef.current?.text ?? textRef.current);
    window.addEventListener("pagehide", flush);
    return () => {
      window.removeEventListener("pagehide", flush);
      flush();
    };
  }, []);

  return {
    text,
    setText,
    beginSubmit: (draft: string) => { pendingRef.current = { text: draft, key: keyRef.current }; },
    finishSubmit: (sent: boolean) => {
      const pending = pendingRef.current;
      pendingRef.current = null;
      if (sent) {
        if (pending?.key && pending.key !== keyRef.current) writeDraft(pending.key, "");
        textRef.current = "";
        writeDraft(keyRef.current, "");
      }
    },
  };
}
