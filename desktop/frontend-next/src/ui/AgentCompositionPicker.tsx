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
  return t(id.startsWith("builtin:") ? "common.builtIn" : id.startsWith("project:") ? "common.project" : "common.personal");
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
  if (unavailable) return <button className="mode composition-trigger" onClick={onSettings}
    data-action="agent-composition.unavailable">{t("agentCompositionPicker.agentPresetsUnavailable")}</button>;
  if (!catalog) return null;
  const selected = catalog.presets.find((v) => v.definition.id === (current?.id ?? "builtin:general"))?.definition;
  const locked = running || statusLocked || !!current?.started;
  const label = current ? compositionName(current.id, current.name) : t(selected?.name ?? "agentCompositionPicker.everydayPartner");
  const items: MenuItem[] = locked && !createNew ? [] : catalog.presets.map(({ definition }) => ({
    value: definition.id,
    label: compositionName(definition.id, definition.name),
    meta: compositionSource(definition.id),
    desc: definition.id.startsWith("builtin:") ? t(definition.description) : definition.description,
    right: locked ? t("common.startANewSession") : undefined,
    disabled: busy,
  }));
  items.push({ value: MANAGE, label: `${t("agentCompositionPicker.manageAgentPresets")}…`, plain: true, divide: true });
  const choose = (id: string) => {
    if (id === MANAGE) { onSettings(); return; }
    if (busy) return;
    if (locked) { createNew?.(id); return; }
    setBusy(true);
    port.selectAgentComposition(id).then((binding) => { setCurrent(binding); onChanged(); })
      .catch((e) => onError(reason(e))).finally(() => setBusy(false));
  };
  return <span className="composition-picker">
    <Picker
      className="mode composition-trigger"
      menuClassName="composer-menu composition-menu"
      ariaLabel={`${t("agentCompositionPicker.agentComposition")}：${label}${current ? ` · ${compositionSource(current.id)}` : ""}${locked ? ` · ${t("common.locked")}` : ""}`}
      data-action={(id) => id === MANAGE ? "agent-composition.manage" : locked ? "agent-composition.new" : "agent-composition.select"}
      title={locked ? t("agentCompositionPicker.thisConversationHasStartedAnd") : selected?.description ? (selected.id.startsWith("builtin:") ? t(selected.description) : selected.description) : t("agentCompositionPicker.chooseAnAgentCompositionBefore")}
      pending={busy}
      label={<><span className="nm">{label}</span><StudioIcon name="down" /></>}
      menuTitle={<><b>{t("common.agentPresets")}</b><small>{t(locked ? "common.locked" : "common.switchBeforeTheFirstMessage")}</small></>}
      head={<>{locked && <p className="composition-menu-note">{t("agentCompositionPicker.thisConversationHasStartedAnd")}</p>}{current && current.resourceMode !== "live" && <p className="composition-menu-note">{t("agentCompositionPicker.resourcesFixedToASaved")}</p>}</>}
      items={items}
      current={locked ? undefined : current?.id ?? "builtin:general"}
      onPick={choose}
      place="bottom"
    />
    {current?.unavailable && <span role="alert" title={current.unavailable}>{t("agentCompositionPicker.compositionResourcesAreUnavailableTh")}</span>}
  </span>;
}
