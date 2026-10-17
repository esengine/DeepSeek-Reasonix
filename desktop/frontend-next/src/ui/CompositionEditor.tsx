import { useEffect, useState } from "react";
import { t } from "../i18n";
import type { AgentComposition, AgentPresetCatalog,CompositionModule, CompositionKind, CompositionSelection } from "../port/composition";
import { COMPOSITION_KINDS } from "../port/composition";
import { Switch } from "./Switch";

const labels: Record<CompositionKind, string> = { tools: "工具", skills: "技能", mcp: "MCP 服务", extensions: "扩展", hooks: "自动化 Hook", commands: "斜杠命令" };

export function CompositionEditor({ draft, modules,disabled, onChange, onValidityChange }: { draft: AgentComposition; modules:AgentPresetCatalog["modules"];disabled: boolean; onChange: (d: AgentComposition) => void; onValidityChange: (valid:boolean)=>void }) {
  const [valid, setValid] = useState<Record<string,boolean>>({});
  useEffect(() => onValidityChange(Object.values(valid).every(Boolean)), [valid,onValidityChange]);
  const persona = draft.persona;
  const updatePersona = (patch: Partial<AgentComposition["persona"]>) => onChange({ ...draft, persona: { ...persona, ...patch } });
  return <div className="composition-editor">
    <label className="pers-field"><span className="lb">{t("预设名称")}</span><input className="in" aria-label={t("预设名称")} data-action-change="agent-preset.name" disabled={disabled} value={draft.id.startsWith("builtin:") ? t(draft.name) : draft.name} onChange={(e) => onChange({ ...draft, name: e.target.value })} /></label>
    <label className="pers-field"><span className="lb">{t("预设说明")}</span><input className="in" data-action-change="agent-preset.description" disabled={disabled} value={draft.id.startsWith("builtin:") ? t(draft.description) : draft.description} onChange={(e) => onChange({ ...draft, description: e.target.value })} /></label>
    <h4>{t("系统人设")}</h4>
    <label className="pers-field"><span className="lb">{t("系统提示词开头")}</span><textarea aria-label={t("系统提示词开头")} data-action-change="persona.prefix" rows={4} maxLength={32768} disabled={disabled} value={persona.prefix} onChange={(e) => updatePersona({ prefix: e.target.value })} /></label>
    <label className="pers-field"><span className="lb">{t("第一方指令之后")}</span><textarea aria-label={t("第一方指令之后")} data-action-change="persona.suffix" rows={3} maxLength={32768} disabled={disabled} value={persona.suffix} onChange={(e) => updatePersona({ suffix: e.target.value })} /></label>
    <div className="lrow"><span className="tx"><span className="lb">{t("用前缀替换整个系统提示词（complete）")}</span><span className="ds">{t("仅保留前缀文本，工具仍受组合和现有权限约束。")}</span></span><Switch data-action="persona.complete" label={t("用前缀替换整个系统提示词（complete）")} on={persona.complete} busy={disabled || (!persona.complete && !persona.prefix.trim())} onClick={() => updatePersona({ complete: !persona.complete })} /></div>
    <div className="lrow"><span className="tx"><span className="lb">{t("注入运行环境与离线提示")}</span></span><Switch data-action="persona.runtime-context" label={t("注入运行环境与离线提示")} on={persona.include_runtime_context} busy={disabled} onClick={() => updatePersona({ include_runtime_context: !persona.include_runtime_context })} /></div>
    {COMPOSITION_KINDS.map((kind) => <ModuleEditor key={`${draft.id}:${kind}`} kind={kind} choices={modules?.[kind] ?? []} value={draft[kind]} disabled={disabled} onValidityChange={(ok)=>setValid((prev)=>({...prev,[kind]:ok}))} onChange={(value) => onChange({ ...draft, [kind]: value })} />)}
  </div>;
}

function ModuleEditor({ kind, choices,value, disabled, onChange, onValidityChange }: { kind: CompositionKind; choices:CompositionModule[];value: CompositionSelection; disabled: boolean; onChange: (value: CompositionSelection) => void; onValidityChange:(valid:boolean)=>void }) {
  const [jsonErrors, setJsonErrors] = useState<Record<number, string>>({});
  const [rawParameters,setRawParameters] = useState<Record<number,string>>({});
  const validate = (errors:Record<number,string>) => { setJsonErrors(errors);onValidityChange(Object.values(errors).every((v)=>!v)); };
  const remove = (index:number) => {
    const shift = <T,>(source:Record<number,T>):Record<number,T> => Object.fromEntries(Object.entries(source).filter(([key])=>Number(key)!==index).map(([key,v])=>[Number(key)>index?Number(key)-1:Number(key),v]));
    validate(shift(jsonErrors));setRawParameters(shift(rawParameters));onChange({...value,members:value.members.filter((_,i)=>i!==index)});
  };
  return <fieldset className="composition-modules" disabled={disabled}>
    <legend>{t(labels[kind])}</legend>
    <select aria-label={`${t(labels[kind])} ${t("选择方式")}`} data-action-change="agent-preset.module-mode" data-target={kind} value={value.mode} onChange={(e) => { validate({});setRawParameters({});onChange({ mode: e.target.value as "inherit" | "select", members: [] }); }}>
      <option value="inherit">{t("创建会话时继承当前配置")}</option><option value="select">{t("仅使用所选项（空列表表示禁用）")}</option>
    </select>
    {value.mode === "select" && <>
      {choices.length>0 && <select aria-label={`${t(labels[kind])} ${t("添加模块")}`} data-action-change="agent-preset.module-choose" data-target={kind} value="" onChange={(e)=>{if(e.target.value) onChange({...value,members:[...value.members,{id:e.target.value}]});}}><option value="">{t("添加模块")}</option>{choices.filter((m)=>!value.members.some((s)=>s.id===m.id)).map((m)=><option key={m.id} value={m.id}>{m.id}</option>)}</select>}
      {value.members.map((module, index) => <div className="composition-module" key={index}>
        <div className="composition-toolbar"><input className="in" aria-label={`${t(labels[kind])} ID ${index + 1}`} data-action-change="agent-preset.module-id" data-target={kind} value={module.id} onChange={(e) => onChange({ ...value, members: value.members.map((m, i) => i === index ? { ...m, id: e.target.value } : m) })} /><button type="button" className="act" data-action="agent-preset.module-remove" data-target={kind} onClick={() => remove(index)}>{t("移除")}</button></div>
        {(kind === "mcp" || kind === "extensions") && <label className="pers-field"><span className="ds">{t("独立参数（JSON）；env 和 headers 仅填写 ${NAME} 引用")}</span><textarea aria-label={`${t(labels[kind])} ${t("独立参数")} ${index + 1}`} data-action-change="agent-preset.module-parameters" data-target={kind} rows={3} value={rawParameters[index]??JSON.stringify(module.parameters ?? {}, null, 2)} onChange={(e) => {
          setRawParameters({...rawParameters,[index]:e.target.value});
          try {
            const parsed: unknown = JSON.parse(e.target.value);
            if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) throw new Error("object required");
            validate({ ...jsonErrors, [index]: "" });
            onChange({ ...value, members: value.members.map((m, i) => i === index ? { ...m, parameters: parsed as Record<string, unknown> } : m) });
          } catch { validate({ ...jsonErrors, [index]: t("参数需要有效的 JSON 对象") }); }
        }} />{jsonErrors[index] && <span role="alert">{jsonErrors[index]}</span>}</label>}
      </div>)}
      <button type="button" className="act" data-action="agent-preset.module-add" data-target={kind} onClick={() => onChange({ ...value, members: [...value.members, { id: "" }] })}>{t("添加模块")}</button>
    </>}
  </fieldset>;
}
