import { useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import { HttpError } from "../port/http_error";
import type { AgentPort } from "../port/port";
import type { IsolationCatalog, IsolatedDiscardPreview, IsolationRecoveryTarget } from "../port/isolation";
import { DiscardIsolatedDialog } from "./DiscardIsolatedDialog";
import "../styles/isolation-results.css";

const stateName = (state: string) => ({ preparing: t("准备中"), running: t("运行中"), pending: t("待整合"), applying: t("整合中"), integrated: t("已整合"), discarded: t("已丢弃"), recovery_required: t("需要恢复"), interrupted: t("已中断"), failed: t("失败"), cancelled: t("已取消"), conflict: t("冲突"), ready: t("现场就绪"), acknowledged: t("已确认整合") }[state] ?? state);

export function IsolationResults({ port }: { port: AgentPort }) {
  const [state, setState] = useState<{ port: AgentPort; value: IsolationCatalog } | null>(null);
  const [again, setAgain] = useState(0);
  const [busy, setBusy] = useState(false);
  const [rootAction, setRootAction] = useState(false);
  const [error, setError] = useState("");
  const [preview, setPreview] = useState<{ port: AgentPort; value: IsolatedDiscardPreview } | null>(null);
  const epoch = useRef(0);
  useEffect(() => port.subscribe((event) => {
    if (event.kind === "workspace_changed") { epoch.current++; setState(null); setPreview(null); setAgain((n) => n + 1); }
  }), [port]);
  useEffect(() => {
    const current = ++epoch.current;
    setPreview(null);
    setError("");
    setBusy(false);
    setRootAction(false);
    port.isolatedResults().then(
      (value) => { if (epoch.current === current) setState({ port, value }); },
      (e) => {
        if (epoch.current !== current) return;
        if (e instanceof HttpError && (e.status === 404 || e.status === 501) && !e.reason?.code) {
          setState({ port, value: { enabled: false, results: [], diagnostics: [], recoveryRequired: false } });
        } else setError(reason(e));
      },
    );
    return () => { epoch.current++; };
  }, [port, again]);

  const act = async (action: () => Promise<void>) => {
    const current = epoch.current;
    setBusy(true);
    setError("");
    try { await action(); }
    catch (e) { if (epoch.current === current) setError(reason(e)); }
    finally { if (epoch.current === current) setBusy(false); }
  };
  const askDiscard = (id: string) => {
    const current = epoch.current;
    void act(async () => {
      const value = await port.previewIsolatedDiscard(id);
      if (epoch.current === current) setPreview({ port, value });
    });
  };
  const discard = () => {
    if (!preview || preview.port !== port) return;
    const current = epoch.current;
    const request = { id: preview.value.id, revision: preview.value.revision };
    void act(async () => {
      await port.discardIsolated(request);
      if (epoch.current === current) { setPreview(null); setAgain((n) => n + 1); }
    });
  };
  const recover = ({ resultId, workspace, runtime, revision }: IsolationRecoveryTarget) => {
    const target = { workspace, runtime, revision, ...(resultId ? { resultId } : {}) };
    const current = epoch.current;
    void act(async () => {
      await port.recoverIsolatedIntegration(target);
      if (epoch.current === current) setAgain((n) => n + 1);
    });
  };
  const manageRoot = (action: string) => {
    const root = catalog?.rootScene;
    if (!root?.actions.includes(action)) return;
    const { environmentId, generation, runtime, owner, publicationId } = root;
    const current = epoch.current;
    setRootAction(true);
    void act(async () => {
      try {
        await port.manageRootScene({ environmentId, generation, runtime, owner, publicationId, action, ...(action === "recover" ? { revision: root.recovery?.revision } : {}) });
        if (epoch.current === current) setAgain((n) => n + 1);
      } finally { if (epoch.current === current) setRootAction(false); }
    });
  };
  const catalog = state?.port === port ? state.value : null;
  const retained = catalog?.results.filter((row) => !row.cleanupComplete) ?? [];
  return <div className="box isolation-results">
    <div className="result-actions">
      <button type="button" className="btn btn--small" data-action="isolation.refresh" disabled={busy} onClick={() => setAgain((n) => n + 1)}>{t("刷新")}</button>
      {catalog?.recoveryRequired && <button type="button" className="btn btn--small" data-action="isolation.recover" disabled={busy || !catalog.recovery} onClick={() => { if (catalog.recovery) recover(catalog.recovery); }}>{t("恢复未完成的整合")}</button>}
    </div>
    {!catalog && !error && <p role="status">{t("正在读取隔离成果…")}</p>}
    {catalog && !catalog.enabled && <p>{t("当前内核未开放隔离成果管理。")}</p>}
    {catalog?.rootScene && <div className="result-entry">
      <p>{t("持久隔离现场：{id}", { id: catalog.rootScene.environmentId })}</p>
      <p>{t("执行目录：{path}", { path: catalog.rootScene.liveWorkspace })}</p>
      <p>{t("整合目标：{path}", { path: catalog.rootScene.sourceWorkspace })}</p>
      <p>{stateName(catalog.rootScene.state)} · {t("验证状态未知")}</p>
      {catalog.rootScene.resultId && <p>{t("原成果：{id}", { id: catalog.rootScene.resultId })}</p>}
      <p className="ds">{t("继续聊天会保留当前现场。重新验证会检查封存成果并尝试整合；重新导出会保留旧成果，下一次冻结当前现场。")}</p>
      <div className="result-actions">
        <button type="button" className="btn btn--small" data-action="isolation.root-recheck" disabled={busy || !catalog.rootScene.actions.includes("recheck")} onClick={() => manageRoot("recheck")}>{t("重新验证并整合")}</button>
        <button type="button" className="btn btn--small" data-action="isolation.root-retry" disabled={busy || !catalog.rootScene.actions.includes("retry")} onClick={() => manageRoot("retry")}>{t("准备重新导出")}</button>
        {catalog.rootScene.recovery && <button type="button" className="btn btn--small" data-action="isolation.root-recover" disabled={busy || !catalog.rootScene.actions.includes("recover")} onClick={() => manageRoot("recover")}>{t("恢复源工作区整合")}</button>}
        {rootAction && busy && <button type="button" className="btn btn--small" data-action="isolation.root-cancel" onClick={() => { void port.cancel().catch((e) => setError(reason(e))); }}>{t("取消")}</button>}
      </div>
      {catalog.rootScene.recovery && <details><summary>{t("待恢复的文件")}</summary><ul>{catalog.rootScene.recovery.paths.map((path) => <li key={path}><code>{path}</code></li>)}</ul></details>}
    </div>}
    {catalog?.enabled && retained.length === 0 && !catalog.rootScene && <p>{t("没有待处理的隔离成果。")}</p>}
    {catalog?.recoveryReason && <p className="why">{catalog.recoveryReason}</p>}
    {catalog?.recovery && <details><summary>{t("待恢复的文件")}</summary><ul>{catalog.recovery.paths.map((path) => <li key={path}><code>{path}</code></li>)}</ul></details>}
    {retained.length > 0 && <p className="ds">{t("以下是保留记录的状态快照。验证状态需重新核对，候选检查通过不表示当前工作区已通过。")}</p>}
    {retained.map((row) => <details key={row.id} className="result-entry">
      <summary><span className="fold"><code>{row.id}</code></span><span>{stateName(row.state)}</span><span>{row.activity === "active" ? t("仍在执行") : row.activity === "inactive" ? t("无活动运行") : t("活动状态待确认")}</span></summary>
      {row.resultId && <p>{t("原成果：{id}", { id: row.resultId })}</p>}
      {row.owner && <p>{t("来源会话：{owner}", { owner: row.owner })}</p>}
      {row.parentId && <p>{t("直接父任务：{id}", { id: row.parentId })}</p>}
      {row.targetWorkspace && <p>{t("整合目标：{path}", { path: row.targetWorkspace })}</p>}
      {row.candidateRoot && <p><code>{row.candidateRoot}</code></p>}
      {row.baselineTree && <p>{t("基线：{tree}", { tree: row.baselineTree })}</p>}
      <p>{row.verification === "invalid" ? t("验证证据已失效") : t("验证状态未知")}</p>
      {row.reason && <p className="why">{row.reason}</p>}
      {row.changes.length > 0 && <ul>{row.changes.slice(0, 100).map((change) => <li key={change.Path}><code>{change.Status} {change.Path}</code></li>)}</ul>}
      {row.changes.length > 100 && <p>{t("另有 {n} 项变更，请在候选目录中查看。", { n: row.changes.length - 100 })}</p>}
      {row.recovery && <>
        <ul>{row.recovery.paths.map((path) => <li key={path}><code>{path}</code></li>)}</ul>
        <button type="button" className="btn btn--small" data-action="isolation.recover-result" data-target={row.id} disabled={busy || !row.actions?.includes("recover")} onClick={() => { if (row.recovery) recover(row.recovery); }}>{t("恢复未完成的整合")}</button>
      </>}
      {!row.kind.startsWith("root_") && <button type="button" className="btn btn--small" data-action="isolation.discard-preview" data-target={row.id} disabled={busy || row.activity === "active" || !row.actions?.includes("discard_preview")} onClick={() => askDiscard(row.id)}>{t("预览丢弃")}</button>}
    </details>)}
    {catalog?.diagnostics.map((diagnostic) => <p className="why" key={diagnostic.record}><code>{diagnostic.record}</code> {diagnostic.reason}</p>)}
    {error && <p className="why" role="alert">{error}</p>}
    {preview?.port === port && <DiscardIsolatedDialog preview={preview.value} busy={busy} error={error} onConfirm={discard} onClose={() => { setPreview(null); setError(""); }} />}
  </div>;
}
