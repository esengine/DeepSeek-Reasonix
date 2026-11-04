import { useState } from "react";
import type { AgentPresetCatalog, AgentPresetVersion } from "../port/composition";
import { t } from "../i18n";
import { StudioIcon } from "./StudioIcon";
import { BuiltinPresetCard } from "./BuiltinPresetCard";
import { CompositionPickerSetting } from "./CompositionPickerSetting";
import { setNewTaskPreset, useNewTaskPreset } from "../state/newTaskPreset";

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
      <h2>{t("persona.agent_presets")}</h2>
      <button className="btn" data-primary="" data-action="agent-preset.create" onClick={onCreate}><StudioIcon name="plus" />{t("persona.new_preset")}</button>
      <p>{t("persona.presets_define_which_capabilities_are")}</p>
    </header>
    <CompositionPickerSetting />
    <p className="composition-default-note">{t("persona.click_a_card_to_set")}</p>
    {error && <p className="find" role="alert">{error}</p>}
    {(["builtin", "user", "project"] as const).map((scope) => {
      const items = catalog.presets.filter(v => v.definition.id.startsWith(scope + ":"));
      if (!items.length) return null;
      return <section className="composition-list-group" key={scope} aria-label={t(scope === "builtin" ? "common.built_in" : scope === "user" ? "persona.personal" : "persona.shared_with_project")}>
        <h3>{t(scope === "builtin" ? "common.built_in" : scope === "user" ? "persona.personal" : "persona.shared_with_project")}</h3>
        {scope === "builtin" ? <div className="composition-builtin-grid">{items.map(v =>
          <BuiltinPresetCard key={v.definition.id} version={v} selected={preferred === v.definition.id}
            onDefault={() => setError(setNewTaskPreset(v.definition.id) ? "" : t("persona.could_not_save_the_new"))}
            onSelect={() => onSelect(v)} onCopy={() => onCopy(v)} />)}</div>
          : items.map(v => <PresetRow key={v.definition.id} version={v} onSelect={() => onSelect(v)} onCopy={() => onCopy(v)}
              {...(scope === "user" ? { selected: preferred === v.definition.id,
                onDefault: () => setError(setNewTaskPreset(v.definition.id) ? "" : t("persona.could_not_save_the_new")) } : {})} />)}
      </section>;
    })}
    <details className="composition-how"><summary>{t("persona.how_to_use")}</summary><p>{t("persona.choose_an_agent_mode_above")}</p></details>
  </div>;
}

function PresetRow({ version: { definition: d }, onSelect, onCopy, selected, onDefault }: {
  version: AgentPresetVersion; onSelect: () => void; onCopy: () => void; selected?: boolean; onDefault?: () => void;
}) {
  const builtin = d.id.startsWith("builtin:");
  const name = builtin ? t(d.name) : d.name;
  return <article className="composition-row">
    <button className="composition-row-main" data-action="agent-preset.view" data-target={d.id}
      aria-label={t(builtin ? "persona.view_name" : "persona.edit_name", { name })} onClick={onSelect}>
      <span className="composition-row-icon"><StudioIcon name={d.id === "builtin:minimal" ? "code" : "layers"} /></span>
      <span className="composition-row-copy"><strong>{name}{d.id === "builtin:general" && <small>{t("common.default")}</small>}{selected && <small>{t("persona.new_task_default")}</small>}</strong>
        <span>{builtin ? t(d.description) : d.description}</span></span>
      <StudioIcon name="chevron" />
    </button>
    {onDefault && <button className="act" data-action="agent-preset.new-task-default" data-target={d.id} aria-pressed={!!selected}
      aria-label={t("persona.set_name_as_the_new", { name })} disabled={selected} onClick={onDefault}>{t(selected ? "persona.new_task_default" : "common.make_default")}</button>}
    <button className="act" data-action="agent-preset.copy" data-target={d.id} aria-label={t("persona.duplicate_name", { name })} onClick={onCopy}>{t("common.copy")}</button>
  </article>;
}
