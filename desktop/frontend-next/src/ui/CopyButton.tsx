import { useEffect, useRef, useState } from "react";
import { t } from "../i18n";

// Wails serves the window over a custom scheme on macOS and Linux, so those two
// hosts are not a secure context and navigator.clipboard is undefined there.
// execCommand is deprecated and is still the only path they have.
async function write(text: string) {
  if (navigator.clipboard?.writeText) return navigator.clipboard.writeText(text);
  const carrier = document.createElement("textarea");
  carrier.value = text;
  carrier.readOnly = true;
  carrier.style.cssText = "position:fixed;top:-9999px;opacity:0";
  document.body.append(carrier);
  carrier.select();
  const ok = document.execCommand("copy");
  carrier.remove();
  if (!ok) throw new Error("copy rejected");
}

const SHEETS = "M5.8 5.8V4a1.1 1.1 0 0 1 1.1-1.1H12A1.1 1.1 0 0 1 13.1 4v5.1A1.1 1.1 0 0 1 12 10.2h-1.8M4 5.8h5.1A1.1 1.1 0 0 1 10.2 6.9V12A1.1 1.1 0 0 1 9.1 13.1H4A1.1 1.1 0 0 1 2.9 12V6.9A1.1 1.1 0 0 1 4 5.8Z";
const TICK = "M3.6 8.3 6.7 11.4 12.4 5";

export function CopyButton({ text, className, label: what }: { text: string; className?: string; label?: string }) {
  const [state, setState] = useState<"idle" | "done" | "failed">("idle");
  const timer = useRef<number | null>(null);

  useEffect(() => () => { if (timer.current !== null) window.clearTimeout(timer.current); }, []);

  const copy = () => {
    if (timer.current !== null) window.clearTimeout(timer.current);
    write(text)
      .then(() => setState("done"))
      .catch(() => setState("failed"))
      .finally(() => { timer.current = window.setTimeout(() => setState("idle"), 1600); });
  };

  // A clipboard the host denied is not the same as nothing happening, and the
  // reader is about to try again — so the failure says so instead of staying idle.
  const label = state === "done" ? t("已复制") : state === "failed" ? t("复制不了") : t("复制");

  return (
    <button
      className={className ? `copy ${className}` : "copy"}
      type="button" data-state={state} onClick={copy}
      aria-label={what ?? t("复制这段回答")}
    >
      {/* Drawn on the gutter marks' own 16-unit grid, so the one control that
          sits inside the prose is not the one shape from another set. The tick
          is the outcome, in the same place the offer was. */}
      <svg className="copy-i" viewBox="0 0 16 16" aria-hidden="true">
        <path d={state === "done" ? TICK : SHEETS} />
      </svg>
      <span aria-live="polite">{label}</span>
    </button>
  );
}
