import { useI18n } from "../lib/i18n";

export function AmountToggle({ label, value, hidden, pending, onToggle }: {
  label: string;
  value: string;
  hidden: boolean;
  pending?: boolean;
  onToggle?: () => void;
}) {
  const { t } = useI18n();
  const action = t(hidden ? "status.showAmounts" : "status.hideAmounts");
  const display = hidden ? "•••" : value;
  return <button type="button" className="amount-toggle" title={action}
    aria-label={`${label}: ${display} · ${action}`} disabled={pending || !onToggle}
    onClick={onToggle}>{display}</button>;
}
