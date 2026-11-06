import { t } from "../i18n";
import { COMPOSITION_KINDS, type AgentComposition, type AgentPresetCatalog, type CompositionKind } from "../port/composition";

export function newPreset(base: AgentComposition, catalog: AgentPresetCatalog, copy: boolean): AgentComposition {
  const ids = new Set(catalog.presets.map(v => v.definition.id));
  let id = "user:custom";
  let n = 1;
  while (ids.has(id)) id = `user:custom-${n++}`;
  return {
    ...structuredClone(base), id,
    name: copy ? `${base.id.startsWith("builtin:") ? t(base.name) : base.name} ${t("persona.copy")}` : "",
    description: copy ? base.id.startsWith("builtin:") ? t(base.description) : base.description : "",
  };
}

export const samePreset = (a: AgentComposition, b: AgentComposition) => JSON.stringify(a) === JSON.stringify(b);

export const KIND_LABELS: Record<CompositionKind, string> = {
  tools: "common.tool",
  skills: "settings.skills",
  mcp: "persona.mcp_servers",
  extensions: "ext.extension",
  hooks: "persona.automation_hooks",
  commands: "persona.slash_commands",
};

const differs = (a: unknown, b: unknown) => JSON.stringify(a) !== JSON.stringify(b);

/** Which parts of a preset the draft moved away from the saved one, worded the
 *  way the editor words them, so leaving can say what would be lost. */
export function presetChanges(draft: AgentComposition, saved: AgentComposition): string[] {
  const out: string[] = [];
  if (draft.name !== saved.name || draft.description !== saved.description) out.push(t("persona.preset_name"));
  if (differs(draft.persona, saved.persona)) out.push(t("persona.persona_and_instructions"));
  for (const kind of COMPOSITION_KINDS) {
    if (differs(draft[kind], saved[kind])) out.push(t("persona.n_kind_items", { kind: t(KIND_LABELS[kind]), n: draft[kind].members.length }));
  }
  return out;
}
