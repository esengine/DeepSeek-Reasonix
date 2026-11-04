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
  return t(id.startsWith("builtin:") ? "内置" : id.startsWith("project:") ? "项目" : "个人");
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
  const [selection, setSelection] = useState<{ port: AgentPort; value: CompositionSummary | null } | null>(null);
  const [busy, setBusy] = useState(false);
  const [unavailable, setUnavailable] = useState(false);
  const enabled = useShowsCompositionPicker();
  const createNew = useContext(CompositionNewContext);
  useEffect(() => {
    if (!enabled) return;
    let active = true;
    setUnavailable(false);
    port.agentPresets().then((list) => {
      if (active) setCatalog(list);
    }).catch(() => { if (active) setUnavailable(true); });
    return () => { active = false; };
  }, [enabled, port, running]);
  useEffect(() => {
    if (!enabled || binding !== undefined) return;
    let active = true;
    port.agentComposition().then((value) => {
      if (active) setSelection({ port, value });
    }).catch(() => { if (active) setUnavailable(true); });
    return () => { active = false; };
  }, [enabled, port, running, binding]);
  const current = binding !== undefined ? binding : selection?.port === port ? selection.value : undefined;
  if (!enabled) return null;
  if (unavailable && !current) return <button className="mode composition-trigger" onClick={onSettings}
    data-action="agent-composition.unavailable">{t("Agent 预设暂不可用")}</button>;
  if (current === undefined) return null;
  const selected = catalog?.presets.find((v) => v.definition.id === (current?.id ?? "builtin:general"))?.definition;
  const locked = running || statusLocked || !!current?.started;
  const label = current ? compositionName(current.id, current.name) : t(selected?.name ?? "日常搭档");
  const items: MenuItem[] = locked && !createNew ? [] : (catalog?.presets ?? []).map(({ definition }) => ({
    value: definition.id,
    label: compositionName(definition.id, definition.name),
    meta: compositionSource(definition.id),
    desc: definition.id.startsWith("builtin:") ? t(definition.description) : definition.description,
    right: locked ? t("新建会话") : undefined,
    disabled: busy,
  }));
  items.push({ value: MANAGE, label: `${t("管理 Agent 预设")}…`, plain: true, divide: true });
  const choose = (id: string) => {
    if (id === MANAGE) { onSettings(); return; }
    if (busy) return;
    if (locked) { createNew?.(id); return; }
    setBusy(true);
    port.selectAgentComposition(id).then((value) => { setSelection({ port, value }); onChanged(); })
      .catch((e) => onError(reason(e))).finally(() => setBusy(false));
  };
  return <span className="composition-picker">
    <Picker
      className="mode composition-trigger"
      menuClassName="composer-menu composition-menu"
      ariaLabel={`${t("Agent 组合")}：${label}${current ? ` · ${compositionSource(current.id)}` : ""}${locked ? ` · ${t("已锁定")}` : ""}`}
      data-action={(id) => id === MANAGE ? "agent-composition.manage" : locked ? "agent-composition.new" : "agent-composition.select"}
      title={locked ? t("该会话已开始，模式已锁定。请用其他模式新建会话。") : selected?.description ? (selected.id.startsWith("builtin:") ? t(selected.description) : selected.description) : t("首次提交前可切换 Agent 组合")}
      pending={busy}
      label={<><span className="nm">{label}</span><StudioIcon name="down" /></>}
      menuTitle={<><b>{t("Agent 预设")}</b><small>{t(locked ? "已锁定" : "首次提交前可切换")}</small></>}
      head={<>{locked && <p className="composition-menu-note">{t("该会话已开始，模式已锁定。请用其他模式新建会话。")}</p>}{current && current.resourceMode !== "live" && <p className="composition-menu-note">{t("已固定资源快照")}</p>}</>}
      items={items}
      current={locked ? undefined : current?.id ?? "builtin:general"}
      onPick={choose}
      place="bottom"
    />
    {current?.unavailable && <span role="alert" title={current.unavailable}>{t("组合资源不可用，当前会话仅可查看历史。")}</span>}
  </span>;
}
