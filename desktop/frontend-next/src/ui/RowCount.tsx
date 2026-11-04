import { t } from "../i18n";

const countLabel = (c: { shown: number; of?: number }) =>
  c.of === undefined ? t("app.n_sessions", { n: c.shown }) : t("app.n_total_sessions", { n: c.shown, total: c.of });

export function RowCount({ c }: { c: { shown: number; of?: number } }) {
  if (c.shown === 0 && !c.of) return null;
  const label = countLabel(c);
  return (
    <span className="rowcount" data-pill="" title={label} aria-label={label}>
      {c.of === undefined ? c.shown : `${c.shown}/${c.of}`}
    </span>
  );
}
