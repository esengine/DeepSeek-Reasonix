import { t } from "../i18n";
import type { AgentPort, ApprovalMode } from "../port/port";
import { APPROVALS, approvalNote } from "./approvals";
import { BrowserTools } from "./BrowserTools";
import { ExternalOrigin } from "./ExternalOrigin";
import { Group } from "./Group";
import { Rules } from "./Rules";
import { Sandbox } from "./Sandbox";
import { SectionCard } from "./SectionCard";
import { SegChoice, SettingRow } from "./SettingRow";
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

export function SafetySettings({ port, mode, busy, onMode, onChanged }: Props) {
  return (
    <>
      <SectionCard title={t("settings.approval")}>
        <SettingRow id="approval" title={t("settings.tool_approvals")} desc={t("safety.ask_before_writing_or_running")}>
          <SegChoice label={t("settings.tool_approvals")} value={mode as ApprovalMode} action="tool-approval.mode" disabled={!!busy}
            onPick={onMode} options={APPROVALS.map(([id, name]) => [id, t(name)] as const)} />
        </SettingRow>
        {mode === "yolo" && <p className="note" role="status">{approvalNote(mode)}</p>}
        <Group id="rules" title={t("settings.rules_you_spell_out")} hint={t("safety.allow_and_deny_by_command")}>
          <Rules port={port} onChanged={onChanged} />
        </Group>
        <ExternalOrigin port={port} onChanged={onChanged} />
        <ToolList port={port} />
      </SectionCard>
      <SectionCard title={t("settings.sandbox_and_execution")}>
        <Group id="sandbox" title={t("settings.sandbox")} hint={t("safety.commands_write_only_here")}>
          <Sandbox port={port} onChanged={onChanged} />
        </Group>
        <Group id="shell" title={t("common.which_program_runs_commands")} hint={t("safety.which_program_runs_a_command")}>
          <Shell port={port} onChanged={onChanged} />
        </Group>
        <Group id="write-lease" title={t("session.several_sessions_writing_one_project")} hint={t("safety.when_two_sessions_edit_one")}>
          <WriteLease port={port} />
        </Group>
      </SectionCard>
      <SectionCard title={t("safety.web")}>
        <WebAccess port={port} />
        <BrowserTools port={port} onChanged={onChanged} />
      </SectionCard>
    </>
  );
}
