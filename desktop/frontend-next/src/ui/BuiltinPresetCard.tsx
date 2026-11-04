import { useId, useState } from "react";
import type { AgentPresetVersion } from "../port/composition";
import { t } from "../i18n";
import { StudioIcon } from "./StudioIcon";

export function BuiltinPresetCard({ version: { definition: d }, selected, onDefault, onSelect, onCopy }: {
  version: AgentPresetVersion;
  selected: boolean;
  onDefault: () => void;
  onSelect: () => void;
  onCopy: () => void;
}) {
  const [usage, setUsage] = useState(false);
  const helpId = useId();
  const name = t(d.name);
  return <article className="composition-builtin-card" data-selected={selected ? "" : undefined}>
    <button type="button" className="composition-builtin-select" data-action="agent-preset.new-task-default"
      data-target={d.id} aria-pressed={selected} aria-label={t("persona.set_name_as_the_new", { name })} onClick={onDefault}>
      <span className="composition-builtin-heading"><strong>{name}</strong>
        <span className="composition-builtin-badge">{t(selected ? "persona.new_task_default" : "common.built_in")}</span>
        <code title={d.id}>{d.id.slice("builtin:".length)}</code></span>
      <span className="composition-builtin-description">{t(d.description)}</span>
    </button>
    <footer className="composition-builtin-footer">
      <button type="button" data-action="agent-preset.view" data-target={d.id}
        aria-label={t("persona.view_name", { name })} onClick={onSelect}>{t("persona.mode_details")}</button>
      <button type="button" data-action="agent-preset.usage" data-target={d.id} aria-expanded={usage}
        aria-controls={helpId} aria-label={t("persona.how_to_use_name", { name })} onClick={() => setUsage(v => !v)}>{t("persona.how_to_use")}</button>
      <button type="button" className="composition-builtin-copy" data-action="agent-preset.copy" data-target={d.id}
        aria-label={t("persona.duplicate_name", { name })} title={t("persona.duplicate_as_custom_preset")} onClick={onCopy}><StudioIcon name="copy" /></button>
    </footer>
    <p className="composition-builtin-usage" id={helpId} hidden={!usage}>{t(d.id === "builtin:general"
      ? "persona.everyday_partner_is_the_initial"
      : "persona.choose_lightweight_partner_in_a")}</p>
  </article>;
}
