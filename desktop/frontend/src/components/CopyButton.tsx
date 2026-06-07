import { useState } from "react";
import { Check, Copy } from "lucide-react";
import { useT } from "../lib/i18n";
import { Tooltip } from "./Tooltip";

// CopyButton copies text to the clipboard on click and briefly flips to a check.
// navigator.clipboard works in the webview under the click's user gesture; a
// failure is swallowed (nothing to copy to).
export function CopyButton({
  text,
  getText,
  className,
  label,
  showLabel = Boolean(label),
}: {
  text?: string;
  getText?: () => string | Promise<string>;
  className?: string;
  label?: string;
  showLabel?: boolean;
}) {
  const t = useT();
  const [copied, setCopied] = useState(false);
  const actionLabel = label ?? t("msg.copy");
  const copy = async () => {
    try {
      const value = getText ? await getText() : text ?? "";
      await navigator.clipboard.writeText(value);
      setCopied(true);
      setTimeout(() => setCopied(false), 1200);
    } catch {
      /* clipboard unavailable */
    }
  };
  return (
    <Tooltip label={copied ? t("msg.copied") : actionLabel}>
      <button
        className={`copybtn ${className ?? ""}`}
        onClick={copy}
        aria-label={actionLabel}
        type="button"
      >
        {copied ? <Check size={13} /> : <Copy size={13} />}
        {label && showLabel && <span className="copybtn__label">{copied ? t("msg.copied") : label}</span>}
      </button>
    </Tooltip>
  );
}
