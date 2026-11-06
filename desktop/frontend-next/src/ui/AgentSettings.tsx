import { useContext, useEffect, useState } from "react";
import { t } from "../i18n";
import type { HubPort } from "../port/hub";
import type { AgentPort } from "../port/port";
import { AgentPresets } from "./AgentPresets";
import { Hooks } from "./Hooks";
import { Group } from "./Group";
import { ScheduledRuns } from "./ScheduledRuns";
import { ApplyNote } from "./Group";
import { AskTimeout } from "./AskTimeout";
import { PeersSection } from "./PeersSection";
import { PlanApproval } from "./PlanApproval";
import { ProgressWatch } from "./ProgressWatch";
import { SectionCard } from "./SectionCard";
import { SessionRef } from "./SessionRef";
import { SettingDeep, SettingDeepContext } from "./SettingDeep";
import { SubagentClaims } from "./SubagentClaims";
import { TurnNarrationSetting } from "./TurnNarrationSetting";

interface Props {
  port: AgentPort;
  hub: Pick<HubPort, "tree">;
  persona: boolean;
  onChanged: () => void;
}

// How many of each there are, for the card's rows. A count that cannot be read
// is left out rather than guessed.
function useAutomationCounts(port: AgentPort) {
  const [counts, setCounts] = useState<{ hooks?: number; schedules?: number; paused?: number }>({});
  useEffect(() => {
    let live = true;
    void port.hooks().then((c) => live && setCounts((was) => ({ ...was, hooks: c.hooks.length }))).catch(() => {});
    void port.schedules().then((b) => live && setCounts((was) => ({ ...was, schedules: b.schedules.length, paused: b.schedules.filter((s) => s.status === "paused").length }))).catch(() => {});
    return () => { live = false; };
  }, [port]);
  return counts;
}

export function AgentSettings({ port, hub, persona, onChanged }: Props) {
  const deep = useContext(SettingDeepContext);
  const counts = useAutomationCounts(port);
  const hooksHint = t("settings.hooks_hint") + (counts.hooks ? ` · ${t("settings.n_hooks", { n: counts.hooks })}` : "");
  const runsHint = t("settings.scheduled_runs_hint") + (counts.schedules
    ? ` · ${t("settings.n_scheduled", { n: counts.schedules })}${counts.paused ? t("settings.n_paused", { n: counts.paused }) : ""}`
    : "");
  return (
    <>
      <SectionCard title={t("settings.persona_and_presets")}>
        <div className="srow" data-setting="persona">
          <div className="srow-meta">
            <span className="srow-name">{t("persona.system_persona")}</span>
            <span className="srow-desc">{t("settings.persona_hint")}</span>
            <ApplyNote id="persona" />
          </div>
          <div className="srow-ctl">
            <button type="button" className="spill" data-action="settings.open-page" data-target="agent-presets" onClick={() => deep.go("agent-presets")}>{t("settings.edit_persona")}</button>
          </div>
        </div>
        <SettingDeep id="agent-presets" title={t("persona.agent_presets")} hint={t("settings.presets_hint")} act={t("settings.manage_presets")}>
          <AgentPresets port={port} focusPersona={persona} />
        </SettingDeep>
      </SectionCard>
      <SectionCard title={t("settings.plans_and_questions")}>
        <PlanApproval port={port} />
        <AskTimeout port={port} />
        <TurnNarrationSetting port={port} />
        <ProgressWatch port={port} />
      </SectionCard>
      <SectionCard title={t("settings.collaboration")}>
        <SessionRef port={port} />
        <SubagentClaims port={port} onChanged={onChanged} />
        <PeersSection port={port} hub={hub} />
      </SectionCard>
      <SectionCard title={t("settings.automation")}>
        <Group id="hooks" title={t("settings.hooks")} hint={hooksHint} pill={t("settings.manage_storage")}>
          <Hooks port={port} onChanged={onChanged} />
        </Group>
        <ScheduledRuns port={port} hint={runsHint} pill={t("settings.manage_storage")} />
      </SectionCard>
    </>
  );
}
