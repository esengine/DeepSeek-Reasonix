import { useContext, useEffect, useState } from "react";
import { CompositionNewContext } from "./CompositionNewContext";
import type { AgentPort } from "../port/port";
import type { AgentPresetCatalog, CompositionSummary } from "../port/composition";
import { Picker, type MenuItem } from "./Menu";
import { StudioIcon } from "./StudioIcon";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import { useShowsCompositionPicker } from "../state/compositionPicker";

const MANAGE = "__manage";

function compositionName(id: string, name: string): string {
  return id.startsWith("builtin:") ? t(name) : name;
}

function compositionSource(id: string): string {
  return t(id.startsWith("builtin:") ? "common.built_in" : id.startsWith("project:") ? "common.project" : "persona.personal");
}

export function AgentCompositionPicker({ port, binding, locked: statusLocked, running, onChanged, onSettings, onError }: {
  port: AgentPort;
  binding?: CompositionSummary | null;
  locked?: boolean;
  running: boolean;
  onChanged: () => void;
  onSettings: () => void;
  onError: (e: unknown) => void;
}) {
  const [catalog, setCatalog] = useState<AgentPresetCatalog | null>(null);
  const [current, setCurrent] = useState<CompositionSummary | null>(null);
  const [busy, setBusy] = useState(false);
  const [unavailable, setUnavailable] = useState(false);
  const enabled = useShowsCompositionPicker();
  const createNew = useContext(CompositionNewContext);
  useEffect(() => {
    if (!enabled) return;
    let active = true;
    setUnavailable(false);
    Promise.all([port.agentPresets(), port.agentComposition()]).then(([list, binding]) => {
      if (active) { setCatalog(list); setCurrent(binding); }
    }).catch(() => { if (active) setUnavailable(true); });
    return () => { active = false; };
  }, [enabled, port, running, binding?.digest, binding?.started]);
  if (!enabled) return null;
  if (unavailable) return <button className="mode composition-trigger unavailable" onClick={onSettings}
    data-action="agent-composition.unavailable">{t("persona.agent_presets_unavailable")}</button>;
  if (!catalog) return null;
  const selected = catalog.presets.find((v) => v.definition.id === (current?.id ?? "builtin:general"))?.definition;
  const locked = running || statusLocked || !!current?.started;
  const currentId = current?.id ?? "builtin:general";
  const label = current ? compositionName(current.id, current.name) : t(selected?.name ?? "persona.everyday_partner");
  const items: MenuItem[] = locked && !createNew ? [] : catalog.presets.map(({ definition }) => ({
    value: definition.id,
    label: compositionName(definition.id, definition.name),
    meta: compositionSource(definition.id),
    desc: definition.id.startsWith("builtin:") ? t(definition.description) : definition.description,
    right: locked ? t(definition.id === currentId ? "common.now" : "common.start_a_new_session") : undefined,
    disabled: busy,
  }));
  items.push({ value: MANAGE, label: `${t("persona.manage_agent_presets")}…`, plain: true, divide: true });
  const choose = (id: string) => {
    if (id === MANAGE) { onSettings(); return; }
    if (busy) return;
    if (locked) { if (id !== currentId) createNew?.(id); return; }
    setBusy(true);
    port.selectAgentComposition(id).then((binding) => { setCurrent(binding); onChanged(); })
      .catch((e) => onError(reason(e))).finally(() => setBusy(false));
  };
  return <span className="composition-picker">
    <Picker
      className={locked ? "mode composition-trigger locked" : "mode composition-trigger"}
      menuClassName="composer-menu composition-menu"
      ariaLabel={`${t("persona.agent_composition")}：${label}${current ? ` · ${compositionSource(current.id)}` : ""}${locked ? ` · ${t("persona.locked")}` : ""}`}
      data-action={(id) => id === MANAGE ? "agent-composition.manage" : locked ? "agent-composition.new" : "agent-composition.select"}
      title={locked ? t("persona.this_conversation_has_started_and") : selected?.description ? (selected.id.startsWith("builtin:") ? t(selected.description) : selected.description) : t("persona.choose_an_agent_composition_before")}
      pending={busy}
      label={<>{locked && <StudioIcon name="lock" />}<span className="nm">{label}</span>{!locked && <StudioIcon name="down" />}</>}
      menuTitle={<><b>{t("persona.agent_presets")}</b><small>{t(locked ? "persona.locked" : "persona.switch_before_the_first_message")}</small></>}
      head={<>{locked && <p className="composition-menu-note">{t("persona.this_conversation_has_started_and")}</p>}{current && current.resourceMode !== "live" && <p className="composition-menu-note">{t("persona.resources_fixed_to_a_saved")}</p>}</>}
      items={items}
      current={currentId}
      onPick={choose}
      place="bottom"
    />
    {current?.unavailable && <span role="alert" title={current.unavailable}>{t("persona.composition_resources_are_unavailable_this")}</span>}
  </span>;
}
