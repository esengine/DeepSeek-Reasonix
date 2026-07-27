import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { ChevronDown, ChevronRight, Clipboard, Loader2, RefreshCw } from "lucide-react";
import { app } from "../lib/bridge";
import { asArray } from "../lib/array";
import { useT } from "../lib/i18n";
import type { CapabilityDiagnosticsReport, CapabilityIssue, SettingsTab } from "../lib/types";

export function DiagnosticsSettingsPage({
  onNavigate,
}: {
  onNavigate?: (tab: SettingsTab) => void;
}) {
  const t = useT();
  const [report, setReport] = useState<CapabilityDiagnosticsReport | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [includeRuntime, setIncludeRuntime] = useState(false);
  const [copied, setCopied] = useState(false);
  const [open, setOpen] = useState<Record<string, boolean>>({
    issues: true,
    instructions: false,
    skills: false,
    commands: false,
    hooks: false,
    plugins: false,
    mcp: false,
  });

  const loadSeq = useRef(0);

  const load = useCallback(async (runtime: boolean) => {
    const seq = ++loadSeq.current;
    setLoading(true);
    setError(null);
    try {
      const next = normalizeDiagnosticsReport(await app.CapabilityDiagnostics(runtime));
      // Last-request-wins: ignore stale responses after rapid refresh/toggle.
      if (seq !== loadSeq.current) return;
      setReport(next);
    } catch (err) {
      if (seq !== loadSeq.current) return;
      setError(err instanceof Error ? err.message : String(err));
      setReport(null);
    } finally {
      if (seq === loadSeq.current) {
        setLoading(false);
      }
    }
  }, []);

  useEffect(() => {
    void load(includeRuntime);
  }, [includeRuntime, load]);

  const issuesBySeverity = useMemo(() => {
    const groups: Record<string, CapabilityIssue[]> = { error: [], warning: [], info: [] };
    for (const issue of report?.issues ?? []) {
      const key = issue.severity === "error" || issue.severity === "warning" ? issue.severity : "info";
      groups[key].push(issue);
    }
    return groups;
  }, [report]);

  const copyJSON = async () => {
    if (!report) return;
    try {
      await navigator.clipboard.writeText(JSON.stringify(report, null, 2));
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    } catch {
      setError(t("diag.copyFailed"));
    }
  };

  const toggle = (key: string) => setOpen((prev) => ({ ...prev, [key]: !prev[key] }));

  const goSettings = (tab?: string) => {
    if (!tab || !onNavigate) return;
    const allowed: SettingsTab[] = ["mcp", "skills", "plugins", "hooks"];
    if (allowed.includes(tab as SettingsTab)) {
      onNavigate(tab as SettingsTab);
    }
  };

  return (
    <div className="diag-page">
      <div className="diag-page__toolbar">
        <label className="diag-page__runtime">
          <input
            type="checkbox"
            checked={includeRuntime}
            onChange={(e) => setIncludeRuntime(e.target.checked)}
          />
          <span>{t("diag.includeRuntime")}</span>
        </label>
        <div className="diag-page__actions">
          <button type="button" className="btn btn--ghost" onClick={() => void load(includeRuntime)} disabled={loading}>
            {loading ? <Loader2 size={14} className="spin" /> : <RefreshCw size={14} />}
            <span>{t("diag.refresh")}</span>
          </button>
          <button type="button" className="btn btn--ghost" onClick={() => void copyJSON()} disabled={!report}>
            <Clipboard size={14} />
            <span>{copied ? t("diag.copied") : t("diag.copyJson")}</span>
          </button>
        </div>
      </div>

      <p className="diag-page__hint">{t("diag.hint")}</p>

      {loading && !report && <div className="empty">{t("settings.loading")}</div>}
      {error && <div className="settings-error" role="alert">{error}</div>}

      {report && (
        <>
          <div className="diag-summary">
            <div className="diag-summary__item diag-summary__item--error">
              <strong>{report.summary.errors}</strong>
              <span>{t("diag.errors")}</span>
            </div>
            <div className="diag-summary__item diag-summary__item--warning">
              <strong>{report.summary.warnings}</strong>
              <span>{t("diag.warnings")}</span>
            </div>
            <div className="diag-summary__item diag-summary__item--info">
              <strong>{report.summary.infos}</strong>
              <span>{t("diag.infos")}</span>
            </div>
            <div className="diag-summary__meta">
              <span className="diag-path">{report.root}</span>
              <span>
                {t("diag.counts", {
                  skills: report.summary.skills,
                  commands: report.summary.commands,
                  hooks: report.summary.hooks,
                  plugins: report.summary.plugins,
                  mcp: report.summary.mcp_servers,
                })}
              </span>
            </div>
          </div>

          <section className="diag-section">
            <button type="button" className="diag-section__header" onClick={() => toggle("issues")}>
              {open.issues ? <ChevronDown size={16} /> : <ChevronRight size={16} />}
              <span>{t("diag.issues")} ({report.issues.length})</span>
            </button>
            {open.issues && (
              <div className="diag-section__body">
                {report.issues.length === 0 && <div className="empty">{t("diag.noIssues")}</div>}
                {(["error", "warning", "info"] as const).map((sev) =>
                  issuesBySeverity[sev].length === 0 ? null : (
                    <div key={sev} className={`diag-issue-group diag-issue-group--${sev}`}>
                      <h4>{t(`diag.severity.${sev}` as "diag.severity.error")}</h4>
                      {issuesBySeverity[sev].map((issue, idx) => (
                        <article key={`${issue.code}-${issue.name ?? ""}-${idx}`} className="diag-issue">
                          <header>
                            <code>{issue.code}</code>
                            {issue.name ? <span className="diag-issue__name">{issue.name}</span> : null}
                          </header>
                          <p className="diag-issue__msg">{issue.message}</p>
                          {issue.source ? <p className="diag-path">{issue.source}</p> : null}
                          {issue.remediation ? <p className="diag-issue__fix">{issue.remediation}</p> : null}
                          {issue.settings_tab && onNavigate ? (
                            <button type="button" className="btn btn--ghost btn--small" onClick={() => goSettings(issue.settings_tab)}>
                              {t("diag.gotoSettings")}
                            </button>
                          ) : null}
                        </article>
                      ))}
                    </div>
                  ),
                )}
              </div>
            )}
          </section>

          <Collapsible
            title={t("diag.instructions")}
            count={report.instructions.docs.length}
            open={!!open.instructions}
            onToggle={() => toggle("instructions")}
          >
            {report.instructions.docs.map((d) => (
              <div key={`${d.order}-${d.path}`} className="diag-row">
                <span>{d.order}. [{d.scope}]</span>
                <span className="diag-path">{d.path}</span>
              </div>
            ))}
            {report.instructions.docs.length === 0 && <div className="empty">{t("common.none")}</div>}
          </Collapsible>

          <Collapsible
            title={t("diag.skills")}
            count={report.skills.winners}
            open={!!open.skills}
            onToggle={() => toggle("skills")}
          >
            {report.skills.entries.filter((e) => e.status === "winner").map((e) => (
              <div key={`${e.name}-${e.path}`} className="diag-row">
                <span>{e.name}</span>
                <span className="diag-path">{e.path}</span>
              </div>
            ))}
          </Collapsible>

          <Collapsible
            title={t("diag.commands")}
            count={report.commands.winners}
            open={!!open.commands}
            onToggle={() => toggle("commands")}
          >
            {report.commands.entries.filter((e) => e.status === "winner").map((e) => (
              <div key={`${e.name}-${e.path}`} className="diag-row">
                <span>/{e.name}</span>
                <span className="diag-path">{e.path}</span>
              </div>
            ))}
          </Collapsible>

          <Collapsible
            title={t("diag.hooks")}
            count={report.hooks.entries.length}
            open={!!open.hooks}
            onToggle={() => toggle("hooks")}
          >
            {report.hooks.entries.map((e, i) => (
              <div key={`${e.event}-${e.source}-${i}`} className="diag-row">
                <span>{e.event} [{e.scope}]</span>
                <span className="diag-path">{e.source}</span>
              </div>
            ))}
          </Collapsible>

          <Collapsible
            title={t("diag.plugins")}
            count={report.plugins.packages.length}
            open={!!open.plugins}
            onToggle={() => toggle("plugins")}
          >
            {report.plugins.packages.map((p) => (
              <div key={p.name} className="diag-row">
                <span>{p.name} ({p.status})</span>
                <span className="diag-path">{p.root}</span>
              </div>
            ))}
          </Collapsible>

          <Collapsible
            title={t("diag.mcp")}
            count={report.mcp.servers.length}
            open={!!open.mcp}
            onToggle={() => toggle("mcp")}
          >
            {report.mcp.servers.map((s) => (
              <div key={s.name} className="diag-row">
                <span>
                  {s.name} · {s.transport} · {s.start_intent}
                  {s.runtime_status ? ` · ${s.runtime_status}` : ""}
                </span>
                <span className="diag-path">{s.source || s.command || s.url_host || ""}</span>
              </div>
            ))}
          </Collapsible>
        </>
      )}
    </div>
  );
}

function normalizeDiagnosticsReport(report: CapabilityDiagnosticsReport): CapabilityDiagnosticsReport {
  return {
    ...report,
    issues: asArray(report.issues),
    instructions: { ...report.instructions, docs: asArray(report.instructions?.docs) },
    skills: {
      ...report.skills,
      roots: asArray(report.skills?.roots),
      entries: asArray(report.skills?.entries),
    },
    commands: {
      ...report.commands,
      roots: asArray(report.commands?.roots),
      entries: asArray(report.commands?.entries),
    },
    hooks: {
      ...report.hooks,
      sources: asArray(report.hooks?.sources),
      entries: asArray(report.hooks?.entries),
    },
    plugins: { ...report.plugins, packages: asArray(report.plugins?.packages) },
    mcp: { ...report.mcp, servers: asArray(report.mcp?.servers) },
  };
}

function Collapsible({
  title,
  count,
  open,
  onToggle,
  children,
}: {
  title: string;
  count: number;
  open: boolean;
  onToggle: () => void;
  children: ReactNode;
}) {
  return (
    <section className="diag-section">
      <button type="button" className="diag-section__header" onClick={onToggle}>
        {open ? <ChevronDown size={16} /> : <ChevronRight size={16} />}
        <span>
          {title} ({count})
        </span>
      </button>
      {open && <div className="diag-section__body">{children}</div>}
    </section>
  );
}
