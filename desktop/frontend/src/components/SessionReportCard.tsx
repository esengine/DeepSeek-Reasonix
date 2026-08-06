import { memo, type ReactNode } from "react";
import { BarChart3, Clock, Coins, FileDiff, MessagesSquare, X } from "lucide-react";
import { formatMoney } from "../lib/money";
import { formatTokens } from "../lib/format";
import { useT } from "../lib/i18n";

export interface SessionReport {
  turns: number;
  promptTokens: number;
  completionTokens: number;
  cacheHitTokens: number;
  cacheMissTokens: number;
  cost?: number;
  currency?: string;
  activeMs: number;
  todosDone: number;
  todosTotal: number;
  edits: number;
  added: number;
  removed: number;
}

function formatDuration(ms: number): string {
  const totalSeconds = Math.max(0, Math.round(ms / 1000));
  const h = Math.floor(totalSeconds / 3600);
  const m = Math.floor((totalSeconds % 3600) / 60);
  const s = totalSeconds % 60;
  if (h > 0) return `${h}h ${m}m`;
  if (m > 0) return `${m}m ${s}s`;
  return `${s}s`;
}

export const SessionReportCard = memo(function SessionReportCard({
  report,
  onClose,
}: {
  report: SessionReport;
  onClose: () => void;
}) {
  const tr = useT();
  const cacheTotal = report.cacheHitTokens + report.cacheMissTokens;
  const cacheRate = cacheTotal > 0 ? Math.round((report.cacheHitTokens / cacheTotal) * 100) : undefined;
  const totalTokens = report.promptTokens + report.completionTokens;
  const hasCost = typeof report.cost === "number" && report.cost > 0;

  const stat = (icon: ReactNode, label: string, value: string, hint?: string) => (
    <div className="session-report__stat">
      <span className="session-report__stat-icon">{icon}</span>
      <span className="session-report__stat-label">{label}</span>
      <span className="session-report__stat-value">{value}</span>
      {hint && <span className="session-report__stat-hint">{hint}</span>}
    </div>
  );

  return (
    <div className="session-report" role="region" aria-label={tr("report.title")}>
      <div className="session-report__head">
        <div className="session-report__title">
          <BarChart3 size={13} />
          <span>{tr("report.title")}</span>
        </div>
        <button type="button" className="session-report__close" aria-label={tr("common.close")} onClick={onClose}>
          <X size={13} />
        </button>
      </div>
      <div className="session-report__grid">
        {stat(
          <MessagesSquare size={13} />,
          tr("report.turns"),
          String(report.turns),
        )}
        {stat(
          <Coins size={13} />,
          tr("report.tokens"),
          formatTokens(totalTokens),
          cacheRate !== undefined ? tr("report.cacheRate", { n: cacheRate }) : undefined,
        )}
        {hasCost &&
          stat(<Coins size={13} />, tr("report.cost"), formatMoney(report.cost, report.currency))}
        {stat(<Clock size={13} />, tr("report.time"), formatDuration(report.activeMs))}
        {stat(
          <FileDiff size={13} />,
          tr("report.edits"),
          String(report.edits),
          report.edits > 0 ? `+${report.added} / −${report.removed}` : undefined,
        )}
        {stat(
          <MessagesSquare size={13} />,
          tr("report.todos"),
          report.todosTotal > 0 ? `${report.todosDone}/${report.todosTotal}` : "—",
        )}
      </div>
    </div>
  );
});
