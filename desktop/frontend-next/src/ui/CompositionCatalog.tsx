import type { AgentPresetCatalog, AgentPresetVersion } from "../port/composition";
import { t } from "../i18n";
import "../styles/composition-presets.css";

export function CompositionCatalog({ catalog, selected, busy, onSelect, onCreate, onDefault }: {
  catalog: AgentPresetCatalog;
  selected?: string;
  busy: boolean;
  onSelect: (version: AgentPresetVersion) => void;
  onCreate: () => void;
  onDefault: (id: string) => void;
}) {
  const builtins = catalog.presets.filter((v) => v.definition.id.startsWith("builtin:"));
  const custom = catalog.presets.filter((v) => !v.definition.id.startsWith("builtin:"));
  return <div className="composition-catalog">
    <h4>{t("内置")}</h4>
    <div className="composition-cards">
      {builtins.map((v) => <article className="composition-card" key={v.definition.id} data-selected={selected === v.definition.id ? "" : undefined}>
        <button type="button" className="composition-card-select" aria-label={t(v.definition.name)} aria-pressed={selected === v.definition.id} disabled={busy} data-action="agent-preset.view" data-target={v.definition.id} onClick={() => onSelect(v)}>
          <span className="composition-card-title"><strong>{t(v.definition.name)}</strong><span className="composition-card-badge">{t(catalog.default === v.definition.id ? "新会话默认" : "内置")}</span></span>
          <span className="composition-card-id">{v.definition.id === "builtin:general" ? "standard" : v.definition.id.slice("builtin:".length)}</span>
          <span className="composition-card-description">{t(v.definition.description)}</span>
        </button>
        <div className="composition-card-footer">
          <button className="act" data-action="agent-preset.view" data-target={v.definition.id} disabled={busy} onClick={() => onSelect(v)}>{t("模式说明")}</button>
          <button className="act" data-action="agent-preset.default" data-target={v.definition.id} disabled={busy || catalog.default === v.definition.id} onClick={() => onDefault(v.definition.id)}>{t(catalog.default === v.definition.id ? "新会话默认" : "设为个人默认")}</button>
        </div>
      </article>)}
    </div>
    <details className="composition-how"><summary>{t("如何使用")}</summary><p>{t("在输入框顶部选择 Agent 模式，首次提交前可切换。开始对话后，请用其他模式新建会话。精简模式的终端仍可修改文件，并非只读模式。")}</p></details>
    <h4>{t("自定义")}</h4>
    {custom.length > 0 && <div className="composition-custom-list">{custom.map((v) => <button className="act" key={v.definition.id} aria-pressed={selected === v.definition.id} disabled={busy} data-action="agent-preset.view" data-target={v.definition.id} onClick={() => onSelect(v)}><span>{v.definition.name}</span><small>{t(v.definition.id.startsWith("project:") ? "项目" : "个人")}{catalog.default === v.definition.id ? ` · ${t("新会话默认")}` : ""}</small></button>)}</div>}
    <button className="composition-create" data-action="agent-preset.create" disabled={busy} onClick={onCreate}>{t("新建自定义预设")}</button>
  </div>;
}
