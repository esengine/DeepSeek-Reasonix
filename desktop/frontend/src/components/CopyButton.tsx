import { useEffect, useRef, useState } from "react";
import { Check, Copy } from "lucide-react";
import { desktopHost } from "../lib/desktopHost";
import { useT } from "../lib/i18n";

function fallbackCopyText(value: string): boolean {
  const activeElement = document.activeElement;
  const selection = document.getSelection();
  const ranges: Range[] = [];
  if (selection) {
    for (let index = 0; index < selection.rangeCount; index += 1) {
      ranges.push(selection.getRangeAt(index));
    }
  }
  const textarea = document.createElement("textarea");
  textarea.value = value;
  textarea.setAttribute("readonly", "");
  textarea.style.position = "fixed";
  textarea.style.inset = "0 auto auto 0";
  textarea.style.width = "1px";
  textarea.style.height = "1px";
  textarea.style.opacity = "0";
  document.body.appendChild(textarea);
  textarea.select();
  let ok = false;
  try {
    ok = document.execCommand("copy");
  } finally {
    textarea.remove();
    if (selection) {
      selection.removeAllRanges();
      for (const range of ranges) selection.addRange(range);
    }
    if (activeElement instanceof HTMLElement) activeElement.focus({ preventScroll: true });
  }
  return ok;
}

async function writeClipboardText(value: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(value);
    return;
  } catch {
    /* try the desktop runtime below */
  }
  try {
    if (await desktopHost().native.clipboardWriteText(value)) return;
  } catch {
    /* host clipboard unavailable in browser dev */
  }
  if (fallbackCopyText(value)) return;
  throw new Error("clipboard unavailable");
}

// CopyButton copies text to the clipboard on click and briefly flips to a check.
// Acknowledgement follows successful content resolution and clipboard write.
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
  const [pending, setPending] = useState(false);
  const [failed, setFailed] = useState(false);
  const epochRef = useRef(0);
  const timerRef = useRef<number | null>(null);
  const actionLabel = label ?? t("msg.copy");
  const stateLabel = pending ? t("chat.loading") : failed ? t("richLink.copyFailed") : copied ? t("msg.copied") : actionLabel;

  useEffect(() => {
    return () => {
      epochRef.current++;
      if (timerRef.current != null) window.clearTimeout(timerRef.current);
    };
  }, []);

  const copy = async () => {
    const epoch = epochRef.current;
    setPending(true); setFailed(false);
    try {
      const value = getText ? await getText() : text ?? "";
      if (epoch !== epochRef.current) return;
      await writeClipboardText(value);
      if (epoch !== epochRef.current) return;
      setCopied(true);
      if (timerRef.current != null) window.clearTimeout(timerRef.current);
      timerRef.current = window.setTimeout(() => {
        setCopied(false);
        timerRef.current = null;
      }, 1200);
    } catch {
      if (epoch === epochRef.current) setFailed(true);
    } finally {
      if (epoch === epochRef.current) setPending(false);
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
      disabled={pending}
      aria-busy={pending}
      aria-label={stateLabel}
      title={stateLabel}
      type="button"
    >
      {copied ? <Check size={13} /> : <Copy size={13} />}
      {showInlineLabel && (
        <span className="copybtn__label-inline">{stateLabel}</span>
      )}
    </button>
  );
}
