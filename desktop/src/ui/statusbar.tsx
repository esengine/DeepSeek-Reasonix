import { useEffect, useRef, useState } from "react";
import { I } from "../icons";
import { t } from "../i18n";
import type { Balance, Settings, UsageStats } from "../App";
import type { JobInfo } from "../protocol";
import { THEME, THEME_STYLES, type Theme, type ThemeStyle, themeForStyle } from "../theme";
import { localizeShortcutText } from "./shortcut";

const USD_TO_CNY = 7.2;

function formatMoney(amountUsd: number, currency: "CNY" | "USD"): string {
  const symbol = currency === "CNY" ? "¥" : "$";
  const amount = currency === "CNY" ? amountUsd * USD_TO_CNY : amountUsd;
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
  themeStyle,
  jobs,
  jobsOpen,
  onToggleJobs,
  onSetThemeStyle,
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
  themeStyle: ThemeStyle;
  jobs: JobInfo[];
  jobsOpen: boolean;
  onToggleJobs: () => void;
  onSetThemeStyle: (style: ThemeStyle) => void;
  onToggleCurrency: () => void;
  onOpenSettings: () => void;
  onOpenWorkdir?: (anchor: { bottom: number; left: number }) => void;
}) {
  const totalTokens = usage.cacheHitTokens + usage.cacheMissTokens;
  const cacheHitPct = totalTokens > 0 ? Math.round((usage.cacheHitTokens / totalTokens) * 100) : 0;
  const runningJobs = jobs.filter((j) => j.running).length;
  const spent = formatMoney(usage.totalCostUsd, currency);
  const balanceLabel = balance
    ? `${balance.currency === "USD" ? "$" : "¥"} ${balance.total.toFixed(2)}`
    : "—";
  const connState = !ready ? "off" : busy ? "running" : "online";
  const [themeOpen, setThemeOpen] = useState(false);
  const themePopRef = useRef<HTMLDivElement | null>(null);
  const themeButtonRef = useRef<HTMLSpanElement | null>(null);

  useEffect(() => {
    if (!themeOpen) return;
    const onPointerDown = (event: PointerEvent) => {
      const target = event.target as Node;
      if (themePopRef.current?.contains(target) || themeButtonRef.current?.contains(target)) return;
      setThemeOpen(false);
    };
    window.addEventListener("pointerdown", onPointerDown);
    return () => window.removeEventListener("pointerdown", onPointerDown);
  }, [themeOpen]);

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
      <span className="seg" title={t("statusbar.cacheHit")}>
        <I.zap size={11} style={{ color: "var(--accent)" }} />
        <span>{t("statusbar.cache")}</span>
        <span className="v acc">{cacheHitPct}%</span>
      </span>
      <span className="seg">
        <I.cpu size={11} />
        <span>{t("statusbar.tokens")}</span>
        <span className="v">{tokenLabel(totalTokens)}</span>
      </span>
      <span className="seg">
        <I.coin size={11} />
        <span>{t("statusbar.thisTurn")}</span>
        <span className="v ok">{spent}</span>
      </span>

      <span className="grow" />

      <span
        className={`seg jobs ${jobsOpen ? "active" : ""}`}
        onClick={onToggleJobs}
        title={localizeShortcutText(t("statusbar.jobsTip"))}
      >
        <I.cpu size={11} />
        <span>{t("statusbar.jobs")}</span>
        <span className={runningJobs > 0 ? "v acc" : "v"}>{runningJobs}</span>
      </span>

      {settings?.workspaceDir ? (
        <span
          className="seg"
          title={t("statusbar.switchWorkspace", { workspace: settings.workspaceDir })}
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
      <span className="seg" title={t("statusbar.switchCurrency")} onClick={onToggleCurrency}>
        <I.coin size={11} />
        <span>{t("statusbar.balance")}</span>
        <span className="v ok">{balanceLabel}</span>
      </span>
      <span
        ref={themeButtonRef}
        className={`seg theme-trigger ${themeOpen ? "active" : ""}`}
        title={t("statusbar.switchTheme")}
        onClick={() => setThemeOpen((open) => !open)}
      >
        {theme === THEME.DARK ? <I.moon size={11} /> : <I.sun size={11} />}
        <span className="v">
          {t(`statusbar.themeStyle${themeStyle[0]!.toUpperCase()}${themeStyle.slice(1)}` as any)}
        </span>
      </span>
      {themeOpen ? (
        <div ref={themePopRef} className="theme-pop" role="menu" aria-label={t("settings.themeStyle")}>
          <div className="theme-pop-head">
            <div className="tt">{t("settings.themeStyle")}</div>
            <div className="ss">{t("statusbar.switchTheme")}</div>
          </div>
          <div className="theme-pop-list">
            {THEME_STYLES.map((style) => (
              <button
                key={style}
                type="button"
                className="theme-pop-item"
                data-on={themeStyle === style}
                data-style={style}
                onClick={() => {
                  onSetThemeStyle(style);
                  setThemeOpen(false);
                }}
              >
                <span className="style-swatches" aria-hidden="true">
                  <span />
                  <span />
                  <span />
                </span>
                <span className="txt">
                  <span className="nm">
                    {t(`statusbar.themeStyle${style[0]!.toUpperCase()}${style.slice(1)}` as any)}
                  </span>
                  <span className="md">
                    {themeForStyle(style) === THEME.DARK
                      ? t("statusbar.themeDark")
                      : t("statusbar.themeLight")}
                  </span>
                </span>
                {themeStyle === style ? <I.check size={13} /> : null}
              </button>
            ))}
          </div>
        </div>
      ) : null}
    </footer>
  );
}
