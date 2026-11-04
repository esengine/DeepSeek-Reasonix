import { useState } from "react";
import { AreaChart, BarChart } from "@mantine/charts";
import { Button, Group as MantineGroup, Paper, Stack, Text } from "@mantine/core";
import type { SurfaceDataset } from "../../../port/wire";
import { Workspace } from "./Workspace";
import { t } from "../../../i18n";
import "@mantine/charts/styles.layer.css";

export default function Dashboard({ dataset, label }: { dataset: SurfaceDataset; label?: string }) {
  const [seriesId, setSeriesId] = useState(dataset.series[0].id);
  const [recent, setRecent] = useState(false);
  const [bar, setBar] = useState(false);
  const [table, setTable] = useState(false);
  const series = dataset.series.find(s => s.id === seriesId) ?? dataset.series[0];
  const start = recent ? Math.floor(dataset.categories.length / 2) : 0;
  const values = series.values.slice(start);
  const total = values.reduce((a, b) => a + b, 0);
  const rows = dataset.categories.slice(start).map((category, i) => ({ category, metric: values[i] }));
  const format = (v: number) => v.toLocaleString(undefined, { maximumFractionDigits: 2 });
  const chart = { h: 320, data: rows, dataKey: "category", series: [{ name: "metric", label: series.label, color: "teal.6" }], valueFormatter: format, withLegend: false, withAnimation: false };
  return <Workspace title={label} badge={t("chat.data_dashboard")} subtitle={t("chat.explore_metrics_and_ranges_with")}>
    <MantineGroup justify="space-between"><MantineGroup gap="xs">{dataset.series.map(s => <Button key={s.id} size="xs" variant={s.id === series.id ? "light" : "subtle"} data-action="surface.explore" onClick={() => setSeriesId(s.id)}>{s.label}</Button>)}</MantineGroup><MantineGroup gap="xs"><Button size="xs" variant={!recent ? "light" : "subtle"} data-action="surface.explore" onClick={() => setRecent(false)}>{t("chat.all_data")}</Button><Button size="xs" variant={recent ? "light" : "subtle"} data-action="surface.explore" onClick={() => setRecent(true)}>{t("chat.recent_half")}</Button></MantineGroup></MantineGroup>
    <div className="rich-metrics">{[{ label: t("chat.total"), value: total, test: "dashboard-total" }, { label: t("chat.average"), value: total / values.length }, { label: t("chat.latest_value"), value: values[values.length - 1] }].map(k => <Paper key={k.label} className="rich-metric" p="lg" radius="md"><Stack gap={5}><Text size="sm" c="dimmed">{k.label}</Text><Text className="rich-metric-value" fw={600} data-testid={k.test}>{format(k.value)}</Text><Text size="xs" c="dimmed">{series.label}{series.unit ? ` · ${series.unit}` : ""}</Text></Stack></Paper>)}</div>
    <Paper p="md" radius="md" className="rich-chart"><Stack><MantineGroup justify="space-between"><Text fw={500}>{series.label}{series.unit ? ` · ${series.unit}` : ""}</Text><MantineGroup gap="xs"><Button size="xs" variant={!bar ? "light" : "subtle"} data-action="surface.explore" onClick={() => setBar(false)}>{t("chat.line_chart")}</Button><Button size="xs" variant={bar ? "light" : "subtle"} data-action="surface.explore" onClick={() => setBar(true)}>{t("chat.bar_chart")}</Button><Button size="xs" variant={table ? "light" : "subtle"} data-action="surface.explore" onClick={() => setTable(v => !v)}>{t("chat.data_details")}</Button></MantineGroup></MantineGroup>{bar ? <BarChart {...chart} /> : <AreaChart {...chart} curveType="linear" />}</Stack></Paper>
    {table && <div className="rich-table"><table aria-label={label}><thead><tr><th>{t("chat.classifying")}</th><th>{series.label}{series.unit ? ` (${series.unit})` : ""}</th></tr></thead><tbody>{rows.map(r => <tr key={r.category}><th>{r.category}</th><td>{format(r.metric)}</td></tr>)}</tbody></table></div>}
  </Workspace>;
}
