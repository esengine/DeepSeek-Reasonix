import type { ReactNode } from "react";
import { useI18n } from "../lib/i18n";
import { Tooltip } from "./Tooltip";

export function StatusBarAmount({ label, title, value, className, hidden, pending, onToggle, children }: {
  label: string;
  title: string;
  value: string;
  className: string;
  hidden: boolean;
  pending: boolean;
  onToggle?: () => void;
  children: ReactNode;
}) {
  const { t } = useI18n();
  const action = t(hidden ? "status.showAmounts" : "status.hideAmounts");
  const display = hidden ? "•••" : value;
  return (
    <Tooltip label={`${hidden ? label : title} · ${action}`} className="statusbar__metric">
      <button type="button" className={`stat statusbar__amount-toggle ${className}`}
        onClick={onToggle} disabled={pending || !onToggle}
        aria-label={`${label}: ${display} · ${action}`}>
        {children}
        <b className={display === "-" ? "stat__value--empty" : undefined}>{display}</b>
      </button>
    </Tooltip>
  );
}
