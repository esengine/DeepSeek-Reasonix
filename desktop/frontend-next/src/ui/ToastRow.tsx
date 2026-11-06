import type { ReactNode } from "react";
import { StudioIcon, type StudioIconName } from "./StudioIcon";

export type ToastKind = "error" | "note" | "update" | "size";

const ICON: Record<ToastKind, StudioIconName> = {
  error: "warning",
  note: "info",
  update: "download",
  size: "sliders",
};

export function ToastRow({
  kind,
  role,
  className,
  children,
  actions,
  stacked = false,
}: {
  kind: ToastKind;
  role: "alert" | "status";
  className?: string;
  children: ReactNode;
  actions: ReactNode;
  stacked?: boolean;
}) {
  return (
    <div
      className={["toast", className].filter(Boolean).join(" ")}
      role={role}
      data-kind={kind}
      data-stacked={stacked || undefined}
    >
      <span
        className="toast-icon"
        data-kind={kind === "error" ? "error" : "note"}
        data-tone={kind === "error" ? "warn" : kind === "note" ? "ok" : "net"}
      >
        <StudioIcon name={ICON[kind]} />
      </span>
      <div className="toast-body">{children}</div>
      <div className="toast-actions">{actions}</div>
    </div>
  );
}
