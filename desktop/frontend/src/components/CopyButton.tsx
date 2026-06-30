import { useEffect, useRef, useState } from "react";
import { Check, Copy } from "lucide-react";
import { useT } from "../lib/i18n";
import { writeClipboardText } from "../lib/clipboard";

// CopyButton copies text to the clipboard on click and briefly flips to a check.
// Clipboard writes are best-effort across browser dev, Wails, and webviews, so
// the visible acknowledgement stays tied to the user action.
export function CopyButton({
  text,
  getText,
  className,
  label,
  showInlineLabel = true,
}: {
  text?: string;
  getText?: () => string | Promise<string>;
  className?: string;
  label?: string;
  showInlineLabel?: boolean;
}) {
  const t = useT();
  const [copied, setCopied] = useState(false);
  const timerRef = useRef<number | null>(null);
  const actionLabel = label ?? t("msg.copy");
  const stateLabel = copied ? t("msg.copied") : actionLabel;

  useEffect(() => {
    return () => {
      if (timerRef.current != null) window.clearTimeout(timerRef.current);
    };
  }, []);

  const copy = async () => {
    try {
      const value = getText ? await getText() : text ?? "";
      void writeClipboardText(value).catch(() => {});
      setCopied(true);
      if (timerRef.current != null) window.clearTimeout(timerRef.current);
      timerRef.current = window.setTimeout(() => {
        setCopied(false);
        timerRef.current = null;
      }, 1200);
    } catch {
      /* clipboard unavailable */
    }
  };
  return (
    <button
      className={[
        "copybtn",
        copied ? "copybtn--copied" : "",
        className ?? "",
      ].filter(Boolean).join(" ")}
      onClick={copy}
      aria-label={stateLabel}
      title={actionLabel}
      type="button"
    >
      {copied ? <Check size={13} /> : <Copy size={13} />}
      {showInlineLabel && (
        <span className="copybtn__label-inline">{stateLabel}</span>
      )}
    </button>
  );
}
