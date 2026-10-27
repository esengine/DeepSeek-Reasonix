import { useEffect, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort } from "../port/port";
import type { SubagentHandoff, SubagentRun } from "../port/subagent";

export function AgentContextFields({ port, runRef, value, onChange, disabled }: {
  port: AgentPort; runRef: string; value: SubagentHandoff; onChange: (value: SubagentHandoff) => void; disabled: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [reload, setReload] = useState(0);
  const [sources, setSources] = useState<{ runs: SubagentRun[]; loading: boolean; error: string }>({ runs: [], loading: true, error: "" });
  useEffect(() => {
    if (!open) return;
    let current = true;
    setSources({ runs: [], loading: true, error: "" });
    void port.subagents().then((runs) => {
      if (current) setSources({ runs: (runs ?? []).filter((run) => run.ref !== runRef && run.state === "completed"), loading: false, error: "" });
    }).catch((error) => {
      if (current) setSources({ runs: [], loading: false, error: reason(error) });
    });
    return () => { current = false; };
  }, [port, runRef, open, reload]);
  return (
    <div className="agentrun-context">
      <button className="btn btn--small" type="button" data-action="agent.context-toggle" data-target={runRef} aria-expanded={open} onClick={() => setOpen(!open)}>
        {t("交接上下文")}{value.resultRefs.length > 0 ? ` · ${value.resultRefs.length}` : ""}
      </button>
      {open && <fieldset disabled={disabled}>
        <label><span>{t("上下文摘要")}</span>
          <textarea rows={2} data-action="agent.context-summary" data-target={runRef} value={value.summary} onChange={(event) => onChange({ ...value, summary: event.target.value })} />
        </label>
        <span className="agentrun-guidance-note">{t("选择本会话已完成的结果；发送时附上完整快照，超出指导上限会拒绝，不会截断。")}</span>
        <div className="agentrun-context-results">
          {sources.loading && <span>{t("正在读取…")}</span>}
          {sources.error && <p role="alert">{sources.error}</p>}
          {!sources.loading && !sources.error && sources.runs.length === 0 && <span>{t("暂无可交接的已完成结果")}</span>}
          {sources.runs.map((run) => <label key={run.ref} className="agentrun-context-result">
            <input type="checkbox" data-action="agent.context-result" data-target={run.ref} checked={value.resultRefs.includes(run.ref)} onChange={(event) => onChange({ ...value, resultRefs: event.target.checked ? [...value.resultRefs, run.ref] : value.resultRefs.filter((ref) => ref !== run.ref) })} />
            <span>{run.name}<small>{run.ref}</small></span>
          </label>)}
          {value.resultRefs.filter((ref) => !sources.loading && !sources.runs.some((run) => run.ref === ref)).map((ref) => <label key={ref} className="agentrun-context-result">
            <input type="checkbox" data-action="agent.context-result" data-target={ref} checked onChange={() => onChange({ ...value, resultRefs: value.resultRefs.filter((selected) => selected !== ref) })} />
            <span>{ref}<small>{t("结果当前不可用；取消选择或刷新后重试")}</small></span>
          </label>)}
        </div>
        <button className="btn btn--small" type="button" data-action="agent.context-refresh" data-target={runRef} disabled={sources.loading} onClick={() => setReload((v) => v + 1)}>{t("刷新结果")}</button>
      </fieldset>}
    </div>
  );
}
