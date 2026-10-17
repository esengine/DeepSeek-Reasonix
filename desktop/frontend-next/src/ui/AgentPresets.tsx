import { useEffect, useState } from "react";
import type { AgentPort } from "../port/port";
import type { AgentComposition, AgentPresetCatalog, AgentPresetVersion, CompositionSummary } from "../port/composition";
import { builtinCompositions } from "../port/composition";
import { t } from "../i18n";
import { reason,say } from "../i18n/kernel";
import { Group } from "./Group";
import { CompositionEditor } from "./CompositionEditor";
import { CompositionCatalog } from "./CompositionCatalog";

export function AgentPresets({ port }: { port: AgentPort }) {
  const [catalog, setCatalog] = useState<AgentPresetCatalog | null>(null);
  const [version, setVersion] = useState<AgentPresetVersion | null>(null);
  const [draft, setDraft] = useState<AgentComposition | null>(null);
  const [current, setCurrent] = useState<CompositionSummary | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [saved, setSaved] = useState("");
  const [deleting, setDeleting] = useState(false);
  const [exported, setExported] = useState("");
  const [valid,setValid] = useState(true);
  const [editorEpoch,setEditorEpoch] = useState(0);
  const [importDiagnostics,setImportDiagnostics] = useState<AgentPresetCatalog["diagnostics"]>([]);
  const select = (v: AgentPresetVersion) => { setVersion(v); setDraft(structuredClone(v.definition)); setSaved(""); setError(""); setDeleting(false); setExported(""); setImportDiagnostics([]);setValid(true);setEditorEpoch((n)=>n+1); };
  const load = async () => {
    try {
      const [next, binding] = await Promise.all([port.agentPresets(), port.agentComposition()]);
      setCatalog(next); setCurrent(binding);
      const v = next.presets.find((v) => v.definition.id === binding?.id) ?? next.presets.find((v) => v.definition.id === next.default) ?? next.presets[0];
      if (v) select(v);
    } catch (e) { setError(reason(e)); }
  };
  useEffect(() => { void load(); }, [port]);
  const perform = async (work: () => Promise<void>) => { setBusy(true); setError(""); setSaved(""); try { await work(); } catch (e) { setError(reason(e)); } finally { setBusy(false); } };
  const copy = (base: AgentComposition) => {
    const ids = new Set(catalog?.presets.map((v) => v.definition.id));
    let id = "user:custom"; let n = 1;
    while (ids.has(id)) id = `user:custom-${n++}`;
    setVersion(null); setDraft({ ...structuredClone(base), id, name: `${base.id.startsWith("builtin:") ? t(base.name) : base.name} ${t("副本")}` }); setSaved(""); setError(""); setDeleting(false);setImportDiagnostics([]);setValid(true);setEditorEpoch((n)=>n+1);
  };
  const immutable = draft?.id.startsWith("builtin:") ?? true;
  const changed = draft && (!version || JSON.stringify(draft) !== JSON.stringify(version.definition));
  return <Group id="agent-presets" title={t("Agent 预设")} hint={t("为不同会话组合人设、工具、技能与扩展。模型保持独立；已开始的会话保留原版本。") }>
    <div className="box">
      {!catalog ? <><p>{error || t("读取预设中…")}</p><button className="act" data-action="agent-preset.reload" onClick={() => void load()}>{t("重新加载")}</button></> : <>
        <CompositionCatalog catalog={catalog} selected={version?.definition.id} busy={busy} onSelect={select} onCreate={() => copy(builtinCompositions()[0])} onDefault={(id) => void perform(async () => { await port.setDefaultAgentPreset(id, false); setCatalog(await port.agentPresets()); setSaved(t("已设为个人默认预设")); })} />
        <div className="composition-toolbar">
          <button className="act" data-action="agent-preset.copy" disabled={!draft || busy} onClick={() => draft && copy(draft)}>{t("复制为自定义预设")}</button>
          <button className="act" data-action="agent-preset.reload" disabled={busy} onClick={() => void load()}>{t("重新加载")}</button>
        </div>
        {current && <p className="ds">{t("当前会话")}: {current.id.startsWith("builtin:") ? t(current.name) : current.name} · {current.revision.slice(0, 12)} {current.started ? t("已锁定") : t("首次提交前可切换")}{version && current.id === version.definition.id && current.revision !== version.revision ? ` · ${t("当前会话使用旧版")}` : ""}</p>}
        {draft && <>
          {!version && <label className="pers-field"><span className="lb">{t("预设标识")}</span><div className="composition-toolbar"><select aria-label={t("存放范围")} data-action-change="agent-preset.scope" value={draft.id.split(":")[0]} onChange={(e) => setDraft({ ...draft, id: `${e.target.value}:${draft.id.split(":")[1]}` })}><option value="user">{t("个人")}</option><option value="project">{t("项目共享")}</option></select><input className="in" aria-label={t("预设标识")} data-action-change="agent-preset.id" value={draft.id.split(":")[1]} onChange={(e) => setDraft({ ...draft, id: `${draft.id.split(":")[0]}:${e.target.value}` })} /></div></label>}
          {immutable && <p className="ds">{t("内置预设只读，复制后即可编辑。")}</p>}
          <CompositionEditor key={editorEpoch} draft={draft} modules={catalog.modules} disabled={busy || immutable} onValidityChange={setValid} onChange={(d) => { setDraft(d); setSaved(""); }} />
          <div className="composition-toolbar">
            <button className="act" data-action="agent-preset.save" disabled={busy || immutable || !changed || !valid} onClick={() => void perform(async () => {
              const result = await port.saveAgentPreset(draft, version?.revision ?? "");
              setCatalog(await port.agentPresets()); setVersion(result); setDraft(structuredClone(result.definition)); setSaved(t("已保存，新会话使用新版"));
            })}>{t("保存预设")}</button>
            <button className="act" data-action="agent-preset.default" disabled={busy || !version || !!changed || draft.id.startsWith("project:")} onClick={() => void perform(async () => { await port.setDefaultAgentPreset(draft.id, false); setCatalog(await port.agentPresets()); setSaved(t("已设为个人默认预设")); })}>{t("设为个人默认")}</button>
            <button className="act" data-action="agent-preset.workspace-default" disabled={busy || !version || !!changed} onClick={() => void perform(async () => { await port.setDefaultAgentPreset(draft.id, true); setCatalog(await port.agentPresets()); setSaved(t("已设为当前项目默认预设")); })}>{t("设为项目默认")}</button>
            <button className="act" data-action="agent-preset.export" disabled={busy || !version || !!changed} onClick={() => void perform(async () => setExported(JSON.stringify(await port.exportAgentPreset(draft.id), null, 2)))}>{t("导出文件")}</button>
            {!immutable && version && <button className="act" data-action="agent-preset.delete-preview" disabled={busy} onClick={() => setDeleting(true)}>{t("删除预设")}</button>}
          </div>
          {deleting && version && <div className="find"><p>{t("删除后不再供新会话选择，已有会话保留绑定版本。")}</p><button className="act" data-action="agent-preset.delete" onClick={() => void perform(async () => { await port.deleteAgentPreset(version.definition.id, version.revision); await load(); })}>{t("确认删除")}</button><button className="act" data-action="agent-preset.delete-cancel" onClick={() => setDeleting(false)}>{t("取消")}</button></div>}
        </>}
        <label className="pers-field"><span className="lb">{t("导入预设文件（先预览，再保存）")}</span><input type="file" accept="application/json,.json" data-action-change="agent-preset.import" disabled={busy} onChange={(e) => {
          const file = e.target.files?.[0]; if (!file) return;
          void perform(async () => { if(file.size>4*1024*1024) throw {code:"agent_composition.invalid"};const imported = await port.importAgentPreset(JSON.parse(await file.text()) as AgentComposition); copy(imported.definition);setImportDiagnostics(imported.diagnostics); setSaved(t("导入预览已就绪，检查模块后保存。")); });
        }} /></label>
        {exported && <a className="act" data-action="agent-preset.download" download="agent-preset.json" href={`data:application/json;charset=utf-8,${encodeURIComponent(exported)}`}>{t("下载预设 JSON")}</a>}
        {catalog.diagnostics.map((d, i) => <p className="ds" key={i}>{d.module}: {d.detail}</p>)}
        {importDiagnostics.map((d,i)=><p className="ds" key={`import-${i}`}>{d.module}: {say({code:d.code})}</p>)}
      </>}
      {saved && <p className="find" role="status">{saved}</p>}
      {error && <p className="find" data-lvl="err" role="alert">{error}</p>}
    </div>
  </Group>;
}
