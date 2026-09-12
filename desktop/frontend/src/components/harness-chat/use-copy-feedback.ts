// Adapted from Harness c291e7961a: async full-content loading and lifecycle fencing.
import { useCallback, useEffect, useRef, useState } from "react";
import { writeClipboard } from "./clipboard";

export function useCopyFeedback(text: string, getText?: () => Promise<string>) {
  const [copied, setCopied] = useState(false);
  const active = useRef(true);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => { active.current = true; return () => { active.current = false; clearTimeout(timer.current); }; }, []);
  const onCopy = useCallback(() => {
    if (copied) return;
    void (getText ? getText() : Promise.resolve(text)).then(async value => {
      if (!active.current || !await writeClipboard(value) || !active.current) return;
      setCopied(true);
      timer.current = setTimeout(() => { if (active.current) setCopied(false); }, 1000);
    }).catch(() => { /* Full-content failures are surfaced by the content owner. */ });
  }, [copied, getText, text]);
  return { copied, onCopy };
}
