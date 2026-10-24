import { useEffect, useRef, useState, type KeyboardEvent } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort, WriteLeaseSettings } from "../port/port";
import { Switch } from "./Switch";

const coordinationName = (mode: string) => ({ legacy: t("兼容模式"), auto: t("自动隔离"), shared: t("共享工作区"), isolated: t("隔离工作区") }[mode] ?? mode);

export function WriteLease({ port }: { port: AgentPort }) {
  const [loaded, setLoaded] = useState<{ port: AgentPort; value: WriteLeaseSettings } | null>(null);
  const state = loaded?.port === port ? loaded.value : null;
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [wait, setWait] = useState("");
  const epoch = useRef(0);

  useEffect(() => {
    const current = ++epoch.current;
    setLoaded(null);
    setBusy(false);
    setError("");
    port
      .writeLease()
      .then((s) => {
        if (epoch.current !== current) return;
        setLoaded({ port, value: s });
        setWait(String(s.maxWaitMinutes));
      })
      .catch(() => { if (epoch.current === current) setLoaded(null); });
    return () => { epoch.current++; };
  }, [port]);

  if (!state) return <div className="empty">{t("无法读取多会话写入设置。")}</div>;

  const save = async (edit: Parameters<AgentPort["saveWriteLease"]>[0]) => {
    const current = epoch.current;
    setBusy(true);
    setError("");
    try {
      const next = await port.saveWriteLease(edit);
      if (epoch.current === current) { setLoaded({ port, value: next }); setWait(String(next.maxWaitMinutes)); }
    } catch (e) {
      if (epoch.current === current) setError(reason(e));
    } finally {
      if (epoch.current === current) setBusy(false);
    }
  };

  const flip = () => save({ optimistic: !state.optimistic });
  const choose = (collaboration: string) => save({ collaboration });
  const flipIntegration = () => save({ autoIntegrate: !state.autoIntegrate });

  const commitWait = async () => {
    const minutes = Number(wait);
    if (minutes === state.maxWaitMinutes) return;
    await save({ maxWaitMinutes: minutes });
  };
  const enter = (e: KeyboardEvent<HTMLInputElement>) => { if (e.key === "Enter") e.currentTarget.blur(); };
  const waitLabel = t("等待其他会话释放写入权的最长时间");

  return (
    <div className="box">
      <div className="lrow">
        <span className="tx">
          <span className="lb">{t("协作工作方式")}</span>
          <span className="ds">{t("自动隔离并行的可写子任务；共享方式只在实际变更时协调写入；隔离方式要求可用的 worktree。保存后新开的会话采用新值，当前聊天保持原策略。")}</span>
        </span>
        <select aria-label={t("协作工作方式")} data-action-change="write-lease.collaboration" value={state.collaboration ?? "legacy"} disabled={busy || state.collaboration === undefined} onChange={(e) => void choose(e.target.value)}>
          {["legacy", "auto", "shared", "isolated"].map((mode) => <option key={mode} value={mode}>{coordinationName(mode)}</option>)}
        </select>
      </div>
      <p className="ds" role="status">{state.effective ? t("当前聊天：{mode}；子代理并发 {total}，写入并发 {writers}。", { mode: coordinationName(state.effective.collaboration), total: state.effective.maxSubagentConcurrency, writers: state.effective.maxParallelWriters }) : t("无法确认当前聊天的生效策略。")}</p>
      {state.effective?.collaboration === "auto" && state.effective.automaticIsolationFallback && <p className="ds">{t("本机无法为自动隔离限定完整的写入边界，当前聊天的可写子任务改用共享事务。")}</p>}
      {state.effective && state.effective.collaboration !== (state.collaboration ?? "legacy") && <p className="ds">{t("已保存：{mode}。当前聊天尚未切换。", { mode: coordinationName(state.collaboration ?? "legacy") })}</p>}
      {state.effective && state.effective.transactionWaitSeconds > 0 && <p className="ds">{t("当前共享事务最多等待 {seconds} 秒，超时后可重新调度；不会强制释放正在写入的资源。", { seconds: state.effective.transactionWaitSeconds })}</p>}
      {state.autoIntegrate !== undefined && <div className="lrow">
        <span className="tx"><span className="lb">{t("自动整合已验证成果")}</span><span className="ds">{t("新开的会话会自动验证并整合无冲突的隔离成果。关闭后保留成果供手动处理。")}</span></span>
        <Switch data-action="write-lease.auto-integrate" on={state.autoIntegrate} busy={busy} label={t("自动整合已验证成果")} onClick={() => void flipIntegration()} />
      </div>}
      {state.effective?.autoIntegrate !== undefined && <p className="ds">{t("当前聊天自动整合：{value}。", { value: state.effective.autoIntegrate ? t("开") : t("关") })}</p>}
      <details open={(state.collaboration ?? "legacy") === "legacy"}>
        <summary><span className="fold">{t("兼容设置（仅兼容模式使用）")}</span></summary>
      <div className="lrow">
        <span className="tx">
          <span className="lb">{t("乐观模式（多会话并行）")}</span>
          <span className="ds">{t("没声明写入范围的操作（bash、MCP、没写 write_paths 的子代理）不再占住整个工作区，所以同一个项目里的多个会话可以同时工作，不必互相等待。代价：声明了路径的写入（如编辑文件）仍按路径互斥（落在工作区之外的路径只和同一路径的写入互斥），但 bash 和 MCP 的写入没有事前的路径围栏，只在事后观察，两个会话用它们改同一个文件时可能互相覆盖。默认开启；关闭后为严格模式，没声明范围的操作会占住整个工作区。只对之后新开的会话生效。")}</span>
        </span>
        <Switch
          data-action="write-lease.optimistic"
          on={state.optimistic}
          busy={busy}
          label={t("乐观模式（多会话并行）")}
          onClick={() => void flip()}
        />
      </div>
      {state.effective?.collaboration === "legacy" && <p className="ds">{t("当前聊天的兼容设置：乐观写入 {optimistic}；最长等待 {minutes} 分钟（0 表示等待到本轮结束）。", { optimistic: state.effective.optimistic ? t("开") : t("关"), minutes: state.effective.maxWaitMinutes })}</p>}
      <div className="lrow threshold-row">
        <span className="tx">
          <span className="lb">{waitLabel}</span>
          <span className="ds">{t("另一个会话占着要写的路径时，这里的写入会一直等；设了分钟数，超过就失败，并给模型一个 workspace.claim_timeout 的明确原因。默认 0，一直等到本轮结束；想设上限，建议 120。之后新开的会话才采用新值。")}</span>
        </span>
        <div className="threshold-control">
          <span className="unit-field">
            <input type="text" inputMode="numeric" aria-label={waitLabel} data-action-keydown="write-lease.max-wait" data-action-change="write-lease.max-wait" value={wait} disabled={busy} onChange={(e) => setWait(e.target.value.replace(/\D/g, ""))} onBlur={() => void commitWait()} onKeyDown={enter} />
            <i>{t("分钟")}</i>
          </span>
        </div>
      </div>
      </details>
      {error && <div className="why" role="alert">{error}</div>}
    </div>
  );
}
