import { useState } from "react";
import type { AgentPresetCatalog, AgentPresetVersion } from "../port/composition";
import { t } from "../i18n";
import { StudioIcon } from "./StudioIcon";
import { BuiltinPresetCard } from "./BuiltinPresetCard";
import { CompositionPickerSetting } from "./CompositionPickerSetting";
import { setNewTaskPreset, useNewTaskPreset } from "../state/newTaskPreset";
import "../styles/composition-presets.css";

export function CompositionCatalog({ catalog, onSelect, onCreate, onCopy }: {
  catalog: AgentPresetCatalog;
  onSelect: (version: AgentPresetVersion) => void;
  onCreate: () => void;
  onCopy: (version: AgentPresetVersion) => void;
}) {
  const preferred = useNewTaskPreset() ?? "builtin:general";
  const [error, setError] = useState("");
  return <div className="composition-catalog">
    <header className="composition-list-heading">
      <h2>{t("Agent 预设")}</h2>
      <button className="btn" data-primary="" data-action="agent-preset.create" onClick={onCreate}><StudioIcon name="plus" />{t("新建预设")}</button>
      <p>{t("预设决定允许使用哪些能力，实际使用时按需加载当前资源。已开始会话的人设和权限规则保持不变。")}</p>
    </header>
    <CompositionPickerSetting />
    <p className="composition-default-note">{t("点击卡片，设置本机新任务的默认模式；已有会话保持当前配置。")}</p>
    {error && <p className="find" role="alert">{error}</p>}
    {(["builtin", "user", "project"] as const).map((scope) => {
      const items = catalog.presets.filter(v => v.definition.id.startsWith(scope + ":"));
      if (!items.length) return null;
      return <section className="composition-list-group" key={scope} aria-label={t(scope === "builtin" ? "内置" : scope === "user" ? "个人" : "项目共享")}>
        <h3>{t(scope === "builtin" ? "内置" : scope === "user" ? "个人" : "项目共享")}</h3>
        {scope === "builtin" ? <div className="composition-builtin-grid">{items.map(v =>
          <BuiltinPresetCard key={v.definition.id} version={v} selected={preferred === v.definition.id}
            onDefault={() => setError(setNewTaskPreset(v.definition.id) ? "" : t("无法保存新任务默认模式，请检查本地存储权限。"))}
            onSelect={() => onSelect(v)} onCopy={() => onCopy(v)} />)}</div>
          : items.map(v => <PresetRow key={v.definition.id} version={v} onSelect={() => onSelect(v)} onCopy={() => onCopy(v)}
              {...(scope === "user" ? { selected: preferred === v.definition.id,
                onDefault: () => setError(setNewTaskPreset(v.definition.id) ? "" : t("无法保存新任务默认模式，请检查本地存储权限。")) } : {})} />)}
      </section>;
    })}
    <details className="composition-how"><summary>{t("如何使用")}</summary><p>{t("在输入框顶部选择 Agent 模式，首次提交前可切换。开始对话后，请用其他模式新建会话。轻装搭档的终端仍可修改文件，并非只读模式。")}</p></details>
  </div>;
}

function PresetRow({ version: { definition: d }, onSelect, onCopy, selected, onDefault }: {
  version: AgentPresetVersion; onSelect: () => void; onCopy: () => void; selected?: boolean; onDefault?: () => void;
}) {
  const builtin = d.id.startsWith("builtin:");
  const name = builtin ? t(d.name) : d.name;
  return <article className="composition-row">
    <button className="composition-row-main" data-action="agent-preset.view" data-target={d.id}
      aria-label={t(builtin ? "查看 {name}" : "编辑 {name}", { name })} onClick={onSelect}>
      <span className="composition-row-icon"><StudioIcon name={d.id === "builtin:minimal" ? "code" : "layers"} /></span>
      <span className="composition-row-copy"><strong>{name}{d.id === "builtin:general" && <small>{t("默认")}</small>}{selected && <small>{t("新任务默认")}</small>}</strong>
        <span>{builtin ? t(d.description) : d.description}</span></span>
      <StudioIcon name="chevron" />
    </button>
    {onDefault && <button className="act" data-action="agent-preset.new-task-default" data-target={d.id} aria-pressed={!!selected}
      aria-label={t("将{name}设为新任务默认", { name })} disabled={selected} onClick={onDefault}>{t(selected ? "新任务默认" : "设为默认")}</button>}
    <button className="act" data-action="agent-preset.copy" data-target={d.id} aria-label={t("复制 {name}", { name })} onClick={onCopy}>{t("复制")}</button>
  </article>;
}
