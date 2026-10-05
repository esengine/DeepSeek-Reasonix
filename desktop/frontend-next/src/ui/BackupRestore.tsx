import { useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort, BackupApplyResult, BackupCategory, BackupEntry, BackupPlan, BackupPlanItem, BackupPluginRef } from "../port/port";
import { AddPlugin } from "./AddPlugin";

export const CATEGORY_LABEL: Record<BackupCategory, string> = {
  settings: "模型与界面设置",
  extensions: "技能、插件与 MCP",
  memory: "长期记忆",
  automation: "Hooks 与状态栏命令",
  secrets: "API 密钥",
};

const STATUS_LABEL: Record<BackupPlanItem["status"], string> = {
  new: "本机没有",
  changed: "与本机不同",
  same: "与本机相同",
  install: "需安装",
};

const CONSENT_LABEL: Record<string, string> = {
  executes: "我已看过这条命令，允许它在本机运行",
  endpoint: "我确认允许把对话和本机保存的密钥发往这个地址",
  replaces_secret: "我确认用备份里的密钥替换本机已保存的密钥",
  imports: "我已读过这份指令文件，允许它引用其中列出的文件",
};

// The preview is the kernel's: which rows need consent, and why, is decided
// there and rechecked on apply. This panel only collects two separate answers
// per row — take it, and (where asked) allow what it does.
export function BackupRestore({ port, backup, onClose }: { port: AgentPort; backup: BackupEntry; onClose: () => void }) {
  const [pass, setPass] = useState("");
  const [plan, setPlan] = useState<BackupPlan | null>(null);
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [allowed, setAllowed] = useState<Set<string>>(new Set());
  const [result, setResult] = useState<BackupApplyResult | null>(null);
  const [installing, setInstalling] = useState<BackupPluginRef | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const open = async () => {
    setBusy(true);
    setError("");
    try {
      const p = await port.previewBackup(backup.id, pass);
      setPlan(p);
      setPicked(new Set(p.items.filter((i) => i.recommended).map((i) => i.id)));
      setAllowed(new Set());
      setPass("");
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  };

  const flip = (set: Set<string>, id: string) => {
    const next = new Set(set);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    return next;
  };

  const blocked = plan?.items.filter((i) => picked.has(i.id) && i.consent && !allowed.has(i.id)) ?? [];

  const apply = async () => {
    if (!plan) return;
    setBusy(true);
    setError("");
    try {
      const ids = plan.items.filter((i) => picked.has(i.id)).map((i) => i.id);
      const consented = ids.filter((id) => allowed.has(id));
      setResult(await port.applyBackup(plan.planId, ids, consented));
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  };

  if (result) {
    return (
      <div className="bk-restore" data-action="backup.result">
        <p className="acct-note">{t("已恢复 {n} 项。", { n: result.applied.length })}</p>
        {result.failed?.map((f) => (
          <p key={f.id} className="acct-note" data-err="">
            {f.id}：{f.error}
          </p>
        ))}
        {result.reloadError && <p className="acct-note">{t("配置已写入，但当前会话未能重新加载：{e}", { e: result.reloadError })}</p>}
        {!!result.plugins?.length && (
          <>
            <p className="acct-note">{t("以下插件需要走正常的安装确认：")}</p>
            <ul className="bk-list">
              {result.plugins.map((p) => (
                <li key={p.name} className="bk-row">
                  <div className="who">
                    <span className="nm">{p.name}</span>
                    <code className="meta">{p.source}{p.version ? " · " + p.version : ""}</code>
                  </div>
                  <button className="btn sm" data-action="backup.install-plugin" data-target={p.name} disabled={installing?.name === p.name} onClick={() => setInstalling({ ...p })}>
                    {t("安装…")}
                  </button>
                </li>
              ))}
            </ul>
          </>
        )}
        <div className="bk-acts">
          <button className="act" data-action="backup.done" onClick={onClose}>{t("完成")}</button>
        </div>
        {installing && (
          <AddPlugin key={installing.name} port={port} source={installing.source} onClose={() => setInstalling(null)} onInstalled={() => setInstalling((current) => current === installing ? null : current)} />
        )}
      </div>
    );
  }

  if (!plan) {
    return (
      <div className="bk-restore">
        <p className="acct-note">{t("输入备份「{name}」的加密口令，先预览它和本机的差异，不会立即写入。", { name: backup.label || t("未命名备份") })}</p>
        <div className="bk-fields">
          <label className="grow">
            <span>{t("加密口令")}</span>
            <input data-action="backup.passphrase" data-target="open" type="password" autoComplete="current-password" value={pass} onChange={(e) => setPass(e.target.value)} />
          </label>
        </div>
        <div className="bk-acts">
          <button className="act" data-primary data-action="backup.preview" disabled={!pass || busy} onClick={() => void open()}>
            {t(busy ? "正在解密…" : "预览差异")}
          </button>
          <button className="act" data-action="backup.cancel" onClick={onClose}>{t("取消")}</button>
        </div>
        {error && <p className="acct-note" data-err="">{error}</p>}
      </div>
    );
  }

  return (
    <div className="bk-restore">
      {!plan.samePlatform && (
        <div className="find" data-lvl="warn" role="note">
          <span className="t">{t("这份备份来自 {p}", { p: plan.platform })}</span>
          <span className="why">{t("命令和路径在另一种系统上可能无法运行，这类项目默认不勾选。")}</span>
        </div>
      )}
      <ul className="bk-plan">
        {plan.items.map((it) => (
          <li key={it.id} className="bk-item" data-status={it.status} data-consent={it.consent ? "" : undefined}>
            <label className="take">
              <input type="checkbox" checked={picked.has(it.id)} onChange={() => setPicked((s) => flip(s, it.id))} data-action="backup.pick" data-target={it.id} />
              <span className="cat">{t(CATEGORY_LABEL[it.category])}</span>
              <span className="nm">{it.name}</span>
              <span className="st">{t(STATUS_LABEL[it.status])}</span>
            </label>
            {it.summary && <code className="sum">{it.summary}</code>}
            {it.details?.map((d) => (
              <code key={d} className="sum">{d}</code>
            ))}
            {it.content !== undefined && it.content !== "" && <pre className="body">{it.content}</pre>}
            {it.previous && <span className="prev">{t("本机现为：")}<code>{it.previous}</code></span>}
            {!!it.files && <span className="prev">{t("{n} 个文件", { n: it.files })}</span>}
            {it.paths?.filter((p) => !p.exists).map((p) => (
              <span key={p.path} className="warn">{t("本机不存在：{p}", { p: p.path })}</span>
            ))}
            {it.crossPlatform && <span className="warn">{t("来自另一种系统，可能无法运行")}</span>}
            {it.consent && picked.has(it.id) && (
              <label className="allow">
                <input type="checkbox" checked={allowed.has(it.id)} onChange={() => setAllowed((s) => flip(s, it.id))} data-action="backup.allow" data-target={it.id} />
                <span>{t(CONSENT_LABEL[it.consent])}</span>
              </label>
            )}
          </li>
        ))}
      </ul>
      {!!plan.omitted?.length && <p className="acct-note">{t("备份时有 {n} 个文件过大或无法读取，未包含在内。", { n: plan.omitted.length })}</p>}
      <div className="bk-acts">
        <button className="act" data-primary data-action="backup.apply" disabled={busy || picked.size === 0 || blocked.length > 0} onClick={() => void apply()}>
          {t(busy ? "正在恢复…" : "恢复所选 {n} 项", { n: picked.size })}
        </button>
        <button className="act" data-action="backup.cancel" onClick={onClose}>{t("取消")}</button>
        {blocked.length > 0 && <span className="acct-note">{t("还有 {n} 项需要逐项确认", { n: blocked.length })}</span>}
      </div>
      {error && <p className="acct-note" data-err="">{error}</p>}
    </div>
  );
}
