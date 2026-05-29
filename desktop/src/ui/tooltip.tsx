import { type ReactNode, useId, useState } from "react";
import { createPortal } from "react-dom";

type TooltipPlacement = "top" | "bottom";

type TooltipState = {
  left: number;
  top: number;
  placement: TooltipPlacement;
};

export function Tooltip({
  content,
  children,
  className,
  disabled,
}: {
  content?: string | null;
  children: ReactNode;
  className?: string;
  disabled?: boolean;
}) {
  const id = useId();
  const [tip, setTip] = useState<TooltipState | null>(null);
  const text = content?.trim();
  const active = Boolean(text) && !disabled;

  const show = (target: HTMLElement) => {
    if (!active) return;
    const rect = target.getBoundingClientRect();
    const placement: TooltipPlacement = rect.top < 88 ? "bottom" : "top";
    const left = Math.min(window.innerWidth - 16, Math.max(16, rect.left + rect.width / 2));
    const top = placement === "top" ? rect.top - 8 : rect.bottom + 8;
    setTip({ left, top, placement });
  };

  const hide = () => setTip(null);

  return (
    <span
      className={["rx-tooltip-anchor", className].filter(Boolean).join(" ")}
      aria-describedby={tip ? id : undefined}
      onPointerEnter={(event) => show(event.currentTarget)}
      onPointerLeave={hide}
      onFocus={(event) => show(event.currentTarget)}
      onBlur={hide}
    >
      {children}
      {tip && text
        ? createPortal(
            <span
              id={id}
              role="tooltip"
              className="rx-tooltip"
              data-placement={tip.placement}
              style={{ left: tip.left, top: tip.top }}
            >
              {text}
            </span>,
            document.body,
          )
        : null}
    </span>
  );
}
