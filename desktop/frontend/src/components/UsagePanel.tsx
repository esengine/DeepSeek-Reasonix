import { useCallback, useEffect, useState } from "react";
import { Download, Trash2 } from "lucide-react";
import { app } from "../lib/bridge";

// ─── Types ──────────────────────────────────────────────────────────────────

interface Overview {
  requests: number;
  prompt_tokens: number;
  completion_tokens: number;
  cache_hit_tokens: number;
  cache_miss_tokens: number;
  reasoning_tokens: number;
  total_tokens: number;
  cost: number;
  currency: string;
  tpm: number;
  rpm: number;
}

interface ModelRow {
  provider: string;
  model: string;
  requests: number;
  prompt_tokens: number;
  completion_tokens: number;
  cache_hit_tokens: number;
  cache_miss_tokens: number;
  reasoning_tokens: number;
  total_tokens: number;
  cost: number;
  currency: string;
  avg_latency_ms: number;
}

interface TrendPoint {
  date: string;
  requests: number;
  total_tokens: number;
  cost: number;
  currency: string;
}

interface LogEntry {
  ts: string;
  provider: string;
  model: string;
  usage_source: string;
  prompt_tokens: number;
  completion_tokens: number;
  cache_hit_tokens: number;
  cache_miss_tokens: number;
  total_tokens: number;
  cost: number;
  currency: string;
  latency_ms: number;
}

interface UsageOverviewData {
  overview: Overview;
  models: ModelRow[];
}

// ─── Helpers ────────────────────────────────────────────────────────────────

function fmt(n: number): string {
  return n.toLocaleString();
}

function fmtCost(cost: number, currency: string): string {
  return `${currency || "¥"}${cost.toFixed(4)}`;
}

function cacheHitRate(ov: Overview): string {
  const total = ov.cache_hit_tokens + ov.cache_miss_tokens;
  if (total === 0) return "-";
  return `${((ov.cache_hit_tokens / total) * 100).toFixed(1)}%`;
}

function progressBar(ratio: number, width: number): string {
  const clamped = Math.max(0, Math.min(1, ratio));
  const filled = Math.round(clamped * width);
  return "█".repeat(filled) + "░".repeat(width - filled);
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B";
  const units = ["B", "KB", "MB", "GB"];
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  return `${(bytes / Math.pow(1024, i)).toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}

// ─── Component ──────────────────────────────────────────────────────────────

export function UsageSettingsPage() {
  const [days, setDays] = useState(0);
  const [data, setData] = useState<UsageOverviewData | null>(null);
  const [trend, setTrend] = useState<TrendPoint[]>([]);
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [diskUsage, setDiskUsage] = useState(0);

  const refresh = useCallback(async () => {
    try {
      const [ov, tr, lg, du] = await Promise.all([
        app.UsageOverview(days) as Promise<UsageOverviewData>,
        app.UsageTrend(days) as Promise<TrendPoint[]>,
        app.UsageLogs(100, days, "", "") as Promise<{ entries: LogEntry[] }>,
        app.UsageDiskUsage() as Promise<number>,
      ]);
      setData(ov);
      setTrend(tr ?? []);
      setLogs(lg?.entries ?? []);
      setDiskUsage(du ?? 0);
    } catch { /* silent */ }
  }, [days]);

  useEffect(() => { refresh(); }, [refresh]);

  const handleDelete = useCallback(async () => {
    if (!confirm("Delete all usage data? This cannot be undone.")) return;
    await app.DeleteUsageData();
    refresh();
  }, [refresh]);

  const ov = data?.overview;
  const models = data?.models ?? [];
  const maxTokens = trend.reduce((m, p) => Math.max(m, p.total_tokens), 0);

  const handleExport = useCallback(async (format: string) => {
    try {
      const result = await app.UsageLogs(10000, days, "", "") as { entries: LogEntry[] };
      const entries = result?.entries ?? [];
      if (format === "csv") {
        const header = "ts,provider,model,usage_source,prompt_tokens,completion_tokens,cache_hit_tokens,cache_miss_tokens,total_tokens,cost,currency,latency_ms";
        const rows = entries.map(e =>
          [e.ts, e.provider, e.model, e.usage_source, e.prompt_tokens, e.completion_tokens, e.cache_hit_tokens, e.cache_miss_tokens, e.total_tokens, e.cost.toFixed(6), e.currency, e.latency_ms].join(",")
        );
        downloadFile("usage.csv", [header, ...rows].join("\n"), "text/csv");
      } else {
        downloadFile("usage.json", JSON.stringify(entries, null, 2), "application/json");
      }
    } catch { /* silent */ }
  }, [days]);

  return (
    <>
      {/* Toolbar: date range + export + delete */}
      <div className="usage-toolbar">
        <div className="usage-toolbar__range">
          {[0, 7, 30, 90].map((d) => (
            <button
              key={d}
              className={`btn btn--small${days === d ? " btn--primary" : ""}`}
              onClick={() => setDays(d)}
            >
              {d === 0 ? "Today" : `${d}d`}
            </button>
          ))}
        </div>
        <div className="usage-toolbar__actions">
          <span className="usage-toolbar__disk" title="Local disk usage">
            {formatBytes(diskUsage)}
          </span>
          <button className="btn btn--small" onClick={() => handleExport("csv")}>
            <Download size={13} /> CSV
          </button>
          <button className="btn btn--small" onClick={() => handleExport("json")}>
            <Download size={13} /> JSON
          </button>
          <button className="btn btn--small btn--danger" onClick={handleDelete} title="Delete all usage data">
            <Trash2 size={13} /> Delete
          </button>
        </div>
      </div>

      {/* KPI cards */}
      {ov && (
        <div className="settings-section">
          <div className="usage-kpi-grid">
            <KPICard label="Requests" value={fmt(ov.requests)} />
            <KPICard label="Total Tokens" value={fmt(ov.total_tokens)} />
            <KPICard label="Cache Hit Rate" value={cacheHitRate(ov)} />
            <KPICard label="Total Cost" value={fmtCost(ov.cost, ov.currency)} />
            <KPICard label="TPM" value={ov.tpm ? ov.tpm.toFixed(0) : "-"} />
            <KPICard label="RPM" value={ov.rpm ? ov.rpm.toFixed(1) : "-"} />
          </div>
        </div>
      )}

      {/* Per-model table */}
      {models.length > 0 && (
        <div className="settings-section">
          <div className="settings-section__head">
            <div className="settings-section__title">Models</div>
          </div>
          <div className="usage-table-wrap">
            <table className="usage-table">
              <thead>
                <tr>
                  <th>Model</th>
                  <th>Requests</th>
                  <th>Prompt</th>
                  <th>Completion</th>
                  <th>Cache Hit</th>
                  <th>Avg Latency</th>
                  <th>Cost</th>
                </tr>
              </thead>
              <tbody>
                {models.map((m) => (
                  <tr key={`${m.provider}/${m.model}`}>
                    <td>{m.provider ? `${m.provider}/` : ""}{m.model}</td>
                    <td>{fmt(m.requests)}</td>
                    <td>{fmt(m.prompt_tokens)}</td>
                    <td>{fmt(m.completion_tokens)}</td>
                    <td>{fmt(m.cache_hit_tokens)}</td>
                    <td>{m.avg_latency_ms.toFixed(0)}ms</td>
                    <td>{fmtCost(m.cost, m.currency)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* Trend */}
      {trend.length > 0 && (
        <div className="settings-section">
          <div className="settings-section__head">
            <div className="settings-section__title">Token Trend</div>
          </div>
          <div className="usage-trend">
            {trend.map((p) => (
              <div key={p.date} className="usage-trend__row">
                <span className="usage-trend__date">{p.date.slice(5)}</span>
                <span className="usage-trend__bar">{progressBar(maxTokens > 0 ? p.total_tokens / maxTokens : 0, 20)}</span>
                <span className="usage-trend__value">{fmt(p.total_tokens)}</span>
                <span className="usage-trend__cost">{fmtCost(p.cost, p.currency)}</span>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Request log */}
      {logs.length > 0 && (
        <div className="settings-section">
          <div className="settings-section__head">
            <div className="settings-section__title">Recent Requests</div>
          </div>
          <div className="usage-table-wrap">
            <table className="usage-table">
              <thead>
                <tr>
                  <th>Time</th>
                  <th>Model</th>
                  <th>Source</th>
                  <th>Prompt</th>
                  <th>Completion</th>
                  <th>Cache</th>
                  <th>Latency</th>
                  <th>Cost</th>
                </tr>
              </thead>
              <tbody>
                {logs.slice(0, 50).map((e, i) => (
                  <tr key={`${e.ts}-${e.model}-${i}`}>
                    <td>{e.ts.slice(0, 19).replace("T", " ")}</td>
                    <td>{e.provider ? `${e.provider}/` : ""}{e.model}</td>
                    <td>{e.usage_source}</td>
                    <td>{fmt(e.prompt_tokens)}</td>
                    <td>{fmt(e.completion_tokens)}</td>
                    <td>{fmt(e.cache_hit_tokens)}</td>
                    <td>{e.latency_ms}ms</td>
                    <td>{fmtCost(e.cost, e.currency)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* Empty state */}
      {!ov && (
        <div className="settings-section" style={{ textAlign: "center", padding: 32, opacity: 0.5 }}>
          No usage data yet. Start a conversation to see statistics here.
        </div>
      )}
    </>
  );
}

// ─── Sub-components ─────────────────────────────────────────────────────────

function KPICard({ label, value }: { label: string; value: string }) {
  return (
    <div className="usage-kpi-card">
      <div className="usage-kpi-card__label">{label}</div>
      <div className="usage-kpi-card__value">{value}</div>
    </div>
  );
}

function downloadFile(name: string, content: string, mime: string) {
  // In Wails desktop, use native save dialog
  if (typeof window !== "undefined" && (window as any).runtime) {
    void app.SaveFile(name, content);
    return;
  }
  // Browser fallback
  const blob = new Blob([content], { type: mime });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  a.click();
  URL.revokeObjectURL(url);
}
