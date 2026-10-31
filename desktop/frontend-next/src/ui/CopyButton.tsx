import { useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import { StudioIcon } from "./StudioIcon";
import { TooltipButton } from "./TooltipButton";

// Wails serves the window over a custom scheme on macOS and Linux, so those two
// hosts are not a secure context and navigator.clipboard is undefined there.
// execCommand is deprecated and is still the only path they have.
export async function copyText(text: string) {
  // A write can be refused (no permission, a document that lost focus) or
  // simply never settle. A copy that hangs says nothing at all, so it is given
  // a deadline — and the legacy path is tried before the reader is told
  // anything went wrong, because a refused modern API is not a dead clipboard.
  const modern = navigator.clipboard?.writeText?.bind(navigator.clipboard);
  if (modern) {
    try {
      await withDeadline(modern(text), clipboardTimeout);
      return;
    } catch (e) {
      try {
        legacyCopy(text);
        return;
      } catch {
        throw new Error(t("剪贴板不可用，请手动复制：{text}", { text }), { cause: e });
      }
    }
  }
  try {
    legacyCopy(text);
  } catch (e) {
    throw new Error(t("剪贴板不可用，请手动复制：{text}", { text }), { cause: e });
  }
}

// A clipboard write that hangs is a copy that never happened, and the reader
// is left waiting for a confirmation that will not come.
const clipboardTimeout = 1500;

async function withDeadline<T>(work: Promise<T>, ms: number): Promise<T> {
  let timer: number | undefined;
  try {
    return await Promise.race([
      work,
      new Promise<never>((_, reject) => {
        timer = window.setTimeout(() => reject(new Error("clipboard write timed out")), ms);
      }),
    ]);
  } finally {
    if (timer !== undefined) window.clearTimeout(timer);
  }
}

function legacyCopy(text: string) {
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
  const label = state === "done" ? t("已复制") : state === "failed" ? t("复制失败，请重试") : what ?? t("复制回复");
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
      aria-label={ariaLabel ?? what ?? t("复制这段回答")}
      title={label}
    >
      {iconOnly && <StudioIcon name={state === "done" ? "check" : state === "failed" ? "warning" : "copy"} />}
      <span className={iconOnly && !(showFeedback && state !== "idle") ? "sr-only" : undefined} aria-live="polite">{label}</span>
    </Button>
  );
}
