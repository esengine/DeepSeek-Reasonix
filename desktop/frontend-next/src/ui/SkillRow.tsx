import { useState } from "react";
import { t } from "../i18n";
import type { AgentPort, SkillEntry } from "../port/port";
import { Exception } from "./CapabilityScope";
import { Switch } from "./Switch";
import { reason } from "../i18n/kernel";

// One skill, and the two things a reader wants from it: whether the model can
// reach it at all, and which of the layers it was switched at.
const SCOPE: Record<string, string> = { project: "项目", custom: "自定义", global: "我的", builtin: "内置" };

function triggerNote(sk: SkillEntry, implicit: boolean): string {
  if (!sk.enabled) return "";
  const auto = !sk.manual && implicit;
  if (sk.slashName && auto) return "";
  if (sk.slashName) return "只能点名";
  if (auto) return "只能模型自选";
  return "调不到";
}

export function SkillRow({
  sk, implicit, port, onDone, root, onFailed,
}: {
  sk: SkillEntry; implicit: boolean; port: AgentPort; onDone: () => void; root: string;
  onFailed: (why: string) => void;
}) {
  const [busy, setBusy] = useState(false);
  const note = triggerNote(sk, implicit);
  const local = sk.switchScope === "project";
  const act = async (fn: () => Promise<void>) => {
    setBusy(true);
    onFailed("");
    try {
      await fn();
    } catch (e) {
      onFailed(reason(e));
    } finally {
      setBusy(false);
      onDone();
    }
  };
  const toggle = () => act(() => port.setSkillEnabled(sk.name, !sk.enabled, "project", root || undefined));
  return (
    <div className="skrow" data-off={sk.enabled ? undefined : ""} data-local={local ? "" : undefined}>
      <span className="nm">{sk.slashName ? "/" + sk.slashName : sk.name}</span>
      <span className="ds" title={sk.description || undefined}>{sk.description || t("没有写说明")}</span>
      <span className="how">{note && <i className={note === "调不到" ? "w none" : "w"}>{t(note)}</i>}</span>
      <span className="face">
        {sk.subagent && <i className="sa">{t("子代理")}</i>}
        {sk.readOnly && <i className="ro">{t("只读")}</i>}
      </span>
      <span className="sc" title={sk.path}>
        {sk.plugin || t(SCOPE[sk.scope ?? ""] ?? "") || sk.scope}
      </span>
      {local && <Exception onClear={() => act(() => port.clearSkillOverride(sk.name, root || undefined))} busy={busy} />}
      <Switch data-action="skill.enabled" data-target={sk.name} on={sk.enabled} busy={busy} label={t(sk.enabled ? "关闭 {name}" : "启用 {name}", { name: sk.name })} onClick={toggle} />
    </div>
  );
}
