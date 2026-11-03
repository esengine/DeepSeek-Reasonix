import { t } from "../i18n";
import type { AgentComposition, AgentPresetCatalog } from "../port/composition";

export function newPreset(base: AgentComposition, catalog: AgentPresetCatalog, copy: boolean): AgentComposition {
  const ids = new Set(catalog.presets.map(v => v.definition.id));
  let id = "user:custom";
  let n = 1;
  while (ids.has(id)) id = `user:custom-${n++}`;
  return {
    ...structuredClone(base), id,
    name: copy ? `${base.id.startsWith("builtin:") ? t(base.name) : base.name} ${t("presetDraft.copy")}` : "",
    description: copy ? base.id.startsWith("builtin:") ? t(base.description) : base.description : "",
  };
}

export const samePreset = (a: AgentComposition, b: AgentComposition) => JSON.stringify(a) === JSON.stringify(b);
