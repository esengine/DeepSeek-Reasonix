import { useState } from "react";
import { t } from "../../i18n";
import type { ChartSpec } from "./spec";
import { cellText } from "./format";

const SHOWN = 200;

export function ChartTable({ spec, id }: { spec: ChartSpec; id: string }) {
  const [open, setOpen] = useState(false);
  const rows = spec.data.rows.slice(0, SHOWN);
  const rest = spec.data.rows.length - rows.length;
  return (
    <div className="chart-data">
      <button type="button" className="chart-toggle" data-action="chart.data" aria-expanded={open} aria-controls={id} onClick={() => setOpen(!open)}>
        {open ? t("收起数据") : t("查看数据")}
      </button>
      {open && (
        <div className="chart-table" id={id} tabIndex={0} role="region" aria-label={t("图表数据：{title}", { title: spec.title })}>
          <table>
            <thead>
              <tr>{spec.data.columns.map((c) => <th key={c.name} scope="col" data-num={c.type === "number" || undefined}>{c.name}</th>)}</tr>
            </thead>
            <tbody>
              {rows.map((r, i) => (
                <tr key={i}>{r.map((v, c) => <td key={c} data-num={spec.data.columns[c].type === "number" || undefined}>{cellText(v)}</td>)}</tr>
              ))}
            </tbody>
          </table>
          {rest > 0 && <div className="chart-more">{t("另有 {n} 行", { n: rest })}</div>}
        </div>
      )}
    </div>
  );
}
