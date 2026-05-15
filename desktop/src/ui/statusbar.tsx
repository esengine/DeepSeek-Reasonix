import { I } from "../icons";
import { t } from "../i18n";
import type { Balance, Settings, UsageStats } from "../App";
import type { JobInfo } from "../protocol";
import { THEME, type Theme } from "../theme";

function formatMoney(amount: number, currency: "CNY" | "USD"): string {
  const symbol = currency === "CNY" ? "¥" : "$";
  return `${symbol} ${amount.toFixed(4)}`;
}

function tokenLabel(n: number): string {
  if (n >= 1000) return `${(n / 1000).toFixed(1)}k`;
  return `${n}`;
}

export function StatusBar({
  settings,
  balance,
  usage,
  busy,
  ready,
  currency,
  theme,
  jobs,
  jobsOpen,
  onToggleJobs,
  onToggleTheme,
  onToggleCurrency,
  onOpenSettings,
  onOpenWorkdir,
}: {
  settings: Settings | null;
  balance: Balance | null;
  usage: UsageStats;
  busy: boolean;
  ready: boolean;
  currency: "CNY" | "USD";
  theme: Theme;
  jobs: JobInfo[];
  jobsOpen: boolean;
  onToggleJobs: () => void;
  onToggleTheme: () => void;
  onToggleCurrency: () => void;
  onOpenSettings: () => void;
  onOpenWorkdir?: (anchor: { bottom: number; left: number }) => void;
}) {
  const totalTokens = usage.cacheHitTokens + usage.cacheMissTokens;
  const cacheHitPct = totalTokens > 0 ? Math.round((usage.cacheHitTokens / totalTokens) * 100) : 0;
  const spent = formatMoney(usage.totalCostUsd, currency);
  const balanceLabel = balance
    ? `${balance.currency === "USD" ? "$" : "¥"} ${balance.total.toFixed(2)}`
    : "—";
  const connState = !ready ? "off" : busy ? "running" : "online";
  return (
    <footer className="statusbar">
      <span className="seg" title={`API · ${settings?.baseUrl ?? "api.deepseek.com"}`}>
        <span
          className={connState === "off" ? "sw warn" : "sw"}
          style={connState === "off" ? { background: "var(--danger)" } : undefined}
        />
        <span>{settings?.baseUrl?.replace(/^https?:\/\//, "") ?? "api.deepseek.com"}</span>
        <span className="v">{!ready ? t("statusbar.offline") : busy ? t("statusbar.busy") : t("statusbar.online")}</span>
      </span>
      <span className="seg" title="cache hit">
        <I.zap size={11} style={{ color: "var(--accent)" }} />
        <span>cache</span>
        <span className="v acc">{cacheHitPct}%</span>
      </span>
      <span className="seg">
        <I.cpu size={11} />
        <span>tokens</span>
        <span className="v">{tokenLabel(totalTokens)}</span>
      </span>
      <span className="seg">
        <I.coin size={11} />
        <span>本次</span>
        <span className="v ok">{spent}</span>
      </span>

      <span className="grow" />

      {jobs.length > 0 ? (
        <span
          className={`seg jobs ${jobsOpen ? "active" : ""}`}
          onClick={onToggleJobs}
          title={t("statusbar.jobsTip")}
        >
          <I.cpu size={11} />
          <span>{t("statusbar.jobs")}</span>
          <span className="v acc">{jobs.filter((j) => j.running).length}</span>
        </span>
      ) : null}

      {settings?.workspaceDir ? (
        <span
          className="seg"
          title={`切换工作区 · ${settings.workspaceDir}`}
          style={onOpenWorkdir ? { cursor: "pointer" } : undefined}
          onClick={(e) => {
            if (!onOpenWorkdir) return;
            const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
            onOpenWorkdir({ bottom: window.innerHeight - r.top + 6, left: r.left });
          }}
        >
          <I.folder size={11} />
          <span className="v">{settings.workspaceDir.split(/[\\/]/).pop() || "ws"}</span>
        </span>
      ) : null}
      <span
        className="seg"
        title={`model · preset ${settings?.preset ?? "auto"}`}
        onClick={onOpenSettings}
      >
        <I.brain size={11} style={{ color: "var(--violet)" }} />
        <span className="v vio">{settings?.model ?? "—"}</span>
      </span>
      <span className="seg" title="切换货币 (CNY / USD)" onClick={onToggleCurrency}>
        <I.coin size={11} />
        <span>余额</span>
        <span className="v ok">{balanceLabel}</span>
      </span>
      <span className="seg" title="切换主题" onClick={onToggleTheme}>
        {theme === THEME.DARK ? <I.moon size={11} /> : <I.sun size={11} />}
        <span className="v">{theme === THEME.DARK ? "深色" : "浅色"}</span>
      </span>
    </footer>
  );
}
