import { useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import { StudioIcon } from "./StudioIcon";
import { TooltipButton } from "./TooltipButton";

// Wails serves the window over a custom scheme on macOS and Linux, so those two
// hosts are not a secure context and navigator.clipboard is undefined there.
// execCommand is deprecated and is still the only path they have.
export async function copyText(text: string) {
  if (navigator.clipboard?.writeText) return navigator.clipboard.writeText(text);
  const focused = document.activeElement;
  const carrier = document.createElement("textarea");
  carrier.value = text;
  carrier.readOnly = true;
  carrier.style.cssText = "position:fixed;top:-9999px;opacity:0";
  document.body.append(carrier);
  try {
    carrier.select();
    if (!document.execCommand("copy")) throw new Error("copy rejected");
  } finally {
    carrier.remove();
    if (focused instanceof HTMLElement && focused.isConnected) focused.focus({ preventScroll: true });
  }
}

export function CopyButton({ text, iconOnly = false, showFeedback = false, className, label: what, ariaLabel, role }: { text: string; iconOnly?: boolean; showFeedback?: boolean; className?: string; label?: string; ariaLabel?: string; role?: "menuitem" }) {
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
  const label = state === "done" ? t("common.copied") : state === "failed" ? t("common.copy_failed_try_again") : what ?? t("app.copy_response");
  const Button = iconOnly ? TooltipButton : "button";

  return (
    <Button
      className={className ? `copy ${className}` : "copy"}
      type="button"
      role={role}
      data-icon={iconOnly ? "" : undefined}
      data-state={state}
      data-feedback={showFeedback && state !== "idle" ? "" : undefined}
      onClick={copy}
      aria-label={ariaLabel ?? what ?? t("app.copy_this_answer")}
      title={label}
    >
      {iconOnly && <StudioIcon name={state === "done" ? "check" : state === "failed" ? "warning" : "copy"} />}
      <span className={iconOnly && !(showFeedback && state !== "idle") ? "sr-only" : undefined} aria-live="polite">{label}</span>
    </Button>
  );
}
