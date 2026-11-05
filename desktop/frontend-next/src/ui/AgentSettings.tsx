import { useContext } from "react";
import { t } from "../i18n";
import type { HubPort } from "../port/hub";
import type { AgentPort } from "../port/port";
import { AgentPresets } from "./AgentPresets";
import { ApplyNote } from "./Group";
import { AskTimeout } from "./AskTimeout";
import { PeersSection } from "./PeersSection";
import { PlanApproval } from "./PlanApproval";
import { ProgressWatch } from "./ProgressWatch";
import { SectionCard } from "./SectionCard";
import { SessionRef } from "./SessionRef";
import { SettingRow } from "./SettingRow";
import { SettingDeep, SettingDeepContext } from "./SettingDeep";
import { SubagentClaims } from "./SubagentClaims";
import { TurnNarrationSetting } from "./TurnNarrationSetting";

interface Props {
  port: AgentPort;
  hub: Pick<HubPort, "tree">;
  persona: boolean;
  preset: string;
  onChanged: () => void;
}

export function AgentSettings({ port, hub, persona, preset, onChanged }: Props) {
  const deep = useContext(SettingDeepContext);
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
        <SettingRow id="preset" title={t("settings.completion_standard")} desc={t("settings.everyday_questions_stay_lightweight_file")}>
          <span className="now">{preset}</span>
        </SettingRow>
        <ProgressWatch port={port} />
      </SectionCard>
      <SectionCard title={t("settings.collaboration")}>
        <SessionRef port={port} />
        <SubagentClaims port={port} onChanged={onChanged} />
        <PeersSection port={port} hub={hub} />
      </SectionCard>
    </>
  );
}
