import { useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import type { AgentPort, SkillEntry } from "../port/port";
import { Exception } from "./CapabilityScope";
import { Switch } from "./Switch";
import { reason } from "../i18n/kernel";
import { CopyButton } from "./CopyButton";

// One skill, and the two things a reader wants from it: whether the model can
// reach it at all, and which of the layers it was switched at.
const SCOPE: Record<string, string> = { project: "项目", custom: "自定义", global: "我的", builtin: "内置" };

// Which way the model can reach this skill, and whether it can reach it at all.
// The second was being read back off the first by comparing the sentence — a
// judgement that a rewrite of the sentence breaks silently, because nothing
// renders differently until someone notices the mark is gone.
function triggerNote(sk: SkillEntry, implicit: boolean): { text: string; reachable: boolean } {
  if (!sk.enabled) return { text: "", reachable: true };
  const auto = !sk.manual && implicit;
  if (sk.slashName && auto) return { text: "", reachable: true };
  if (sk.slashName) return { text: "只能点名", reachable: true };
  if (auto) return { text: "只能模型自选", reachable: true };
  return { text: "调不到", reachable: false };
}

export function SkillRow({
  sk, implicit, port, onDone, root, onFailed,
}: {
  sk: SkillEntry; implicit: boolean; port: AgentPort; onDone: () => void; root: string;
  onFailed: (why: string) => void;
}) {
  const [owner, setOwner] = useState({ port, root });
  const currentOwner = useRef(owner);
  currentOwner.current = owner;
  const [busy, setBusy] = useState(false);
  if (owner.port !== port || owner.root !== root) {
    setOwner({ port, root });
    setBusy(false);
  }
  useEffect(() => {
    currentOwner.current = owner;
    return () => { currentOwner.current = { ...owner }; };
  }, [owner]);
  const note = triggerNote(sk, implicit);
  const local = sk.switchScope === "project";
  const command = sk.slashName ? "/" + sk.slashName : "";
  const act = async (fn: () => Promise<void>) => {
    setBusy(true);
    onFailed("");
    try {
      await fn();
    } catch (e) {
      if (currentOwner.current === owner) onFailed(reason(e));
    } finally {
      if (currentOwner.current === owner) {
        setBusy(false);
      }
      onDone();
    }
  };
  const toggle = () => act(() => port.setSkillEnabled(sk.name, !sk.enabled, "project", root || undefined));
  return (
    <div className="skrow" data-off={sk.enabled ? undefined : ""} data-local={local ? "" : undefined}>
      <span className="nm">{command || sk.name}</span>
      <span className="ds" title={sk.description || undefined}>{sk.description || t("未提供说明")}</span>
      <span className="how">{note.text && <i className={note.reachable ? "w" : "w none"}>{t(note.text)}</i>}</span>
      <span className="face">
        {sk.subagent && <i className="sa">{t("子代理")}</i>}
        {sk.readOnly && <i className="ro">{t("只读")}</i>}
      </span>
      <span className="sc" title={sk.path}>
        {sk.plugin || t(SCOPE[sk.scope ?? ""] ?? "") || sk.scope}
      </span>
      {command && <CopyButton key={command} text={command} iconOnly label={t("复制调用命令 {command}", { command })} />}
      {local && <Exception onClear={() => act(() => port.clearSkillOverride(sk.name, root || undefined))} busy={busy} />}
      <Switch data-action="skill.enabled" data-target={sk.name} on={sk.enabled} busy={busy} label={t(sk.enabled ? "关闭 {name}" : "启用 {name}", { name: sk.name })} onClick={toggle} />
    </div>
  );
}
