import { useEffect, useState, type ReactNode } from "react";
import { t } from "../i18n";
import type { AgentPort } from "../port/port";
import type { ApprovalMode } from "../port/port";
import { APPROVALS, approvalNote } from "./approvals";
import { BrowserTools } from "./BrowserTools";
import { ExternalOrigin } from "./ExternalOrigin";
import { Rules } from "./Rules";
import { Sandbox } from "./Sandbox";
import { SectionCard } from "./SectionCard";
import { SegChoice, SettingRow } from "./SettingRow";
import { SettingDeep } from "./SettingDeep";
import { Shell } from "./Shell";
import { ToolList } from "./ToolList";
import { WebAccess } from "./WebAccess";
import { WriteLease } from "./WriteLease";

interface Props {
  port: AgentPort;
  mode: ApprovalMode | undefined;
  busy: string;
  onMode: (mode: ApprovalMode) => void;
  onChanged: () => void;
}

const page = (body: ReactNode) => <section className="prefs-card">{body}</section>;

export function SafetySettings({ port, mode, busy, onMode, onChanged }: Props) {
  const [rules, setRules] = useState<number | null>(null);
  const [tools, setTools] = useState<number | null>(null);
  useEffect(() => {
    port.permissions().then((p) => setRules(p.allow.length + p.ask.length + p.deny.length)).catch(() => setRules(null));
    port.toolSettings().then((s) => setTools(s.tools.length)).catch(() => setTools(null));
  }, [port]);
  const details = t("safety.details");
  return (
    <>
      <SectionCard title={t("settings.approval")}>
        <SettingRow id="approval" title={t("settings.tool_approvals")} desc={t("safety.ask_before_writing_or_running")}>
          <SegChoice label={t("settings.tool_approvals")} value={mode as ApprovalMode} action="tool-approval.mode" disabled={!!busy}
            onPick={onMode} options={APPROVALS.map(([id, name]) => [id, t(name)] as const)} />
        </SettingRow>
        {mode === "yolo" && <p className="note" role="status">{approvalNote(mode)}</p>}
        <SettingDeep id="rules" title={t("settings.rules_you_spell_out")} hint={t("safety.allow_and_deny_by_command")}
          act={rules === null ? details : t("safety.edit_rules_n", { n: rules })}>
          {page(<Rules port={port} onChanged={onChanged} />)}
        </SettingDeep>
        <ExternalOrigin port={port} onChanged={onChanged} />
        <SettingDeep id="tool-list" title={t("ext.tools_the_model_can_use")} hint={t("safety.which_tools_the_model_sees")}
          act={tools === null ? details : t("safety.view_and_adjust_n", { n: tools })}>
          {page(<ToolList port={port} />)}
        </SettingDeep>
      </SectionCard>
      <SectionCard title={t("settings.sandbox_and_execution")}>
        <SettingDeep id="sandbox" title={t("settings.sandbox")} hint={t("safety.commands_write_only_here")} act={details}>
          {page(<Sandbox port={port} onChanged={onChanged} />)}
        </SettingDeep>
        <SettingDeep id="shell" title={t("common.which_program_runs_commands")} hint={t("safety.which_program_runs_a_command")} act={details}>
          {page(<Shell port={port} onChanged={onChanged} />)}
        </SettingDeep>
        <SettingDeep id="write-lease" title={t("session.several_sessions_writing_one_project")} hint={t("safety.when_two_sessions_edit_one")} act={details}>
          {page(<WriteLease port={port} />)}
        </SettingDeep>
      </SectionCard>
      <SectionCard title={t("safety.web")}>
        <SettingDeep id="web-access" title={t("models.web_search_and_fetch")} hint={t("safety.search_and_fetch_when_needed")} act={details}>
          {page(<WebAccess port={port} />)}
        </SettingDeep>
        <BrowserTools port={port} onChanged={onChanged} />
      </SectionCard>
    </>
  );
}
