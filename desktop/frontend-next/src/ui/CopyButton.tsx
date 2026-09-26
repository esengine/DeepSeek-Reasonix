import { useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import { StudioIcon } from "./StudioIcon";

// Wails serves the window over a custom scheme on macOS and Linux, so those two
// hosts are not a secure context and navigator.clipboard is undefined there.
// execCommand is deprecated and is still the only path they have.
export async function copyText(text: string) {
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

export function CopyButton({ text, iconOnly = false, className, label: what }: { text: string; iconOnly?: boolean; className?: string; label?: string }) {
  const [state, setState] = useState<"idle" | "done" | "failed">("idle");
  const timer = useRef<number | null>(null);

  useEffect(() => () => { if (timer.current !== null) window.clearTimeout(timer.current); }, []);

  const copy = () => {
    if (timer.current !== null) window.clearTimeout(timer.current);
    copyText(text)
      .then(() => setState("done"))
      .catch(() => setState("failed"))
      .finally(() => { timer.current = window.setTimeout(() => setState("idle"), 1600); });
  };

  // A clipboard the host denied is not the same as nothing happening, and the
  // reader is about to try again — so the failure says so instead of staying idle.
  const label = state === "done" ? t("已复制") : state === "failed" ? t("复制不了") : what ?? t("复制回复");

  return (
    <button
      className={className ? `copy ${className}` : "copy"}
      type="button"
      data-icon={iconOnly ? "" : undefined}
      data-state={state}
      onClick={copy}
      aria-label={what ?? t("复制这段回答")}
      title={label}
    >
      {iconOnly && <StudioIcon name={state === "done" ? "check" : "copy"} />}
      <span className={iconOnly ? "sr-only" : undefined} aria-live="polite">{label}</span>
    </button>
  );
}
