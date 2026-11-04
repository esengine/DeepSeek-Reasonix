import { t } from "../i18n";
import type { AgentPort } from "../port/port";
import { Group } from "./Group";
import { SubagentClaims } from "./SubagentClaims";
import { WriteLease } from "./WriteLease";

export function SessionCoordination({ port, onChanged }: { port: AgentPort; onChanged: () => void }) {
  return (
    <>
      <Group id="write-lease"
        title={t("session.several_sessions_writing_one_project")}
        hint={t("session.how_sessions_in_one_project")}
      >
        <WriteLease port={port} />
      </Group>
      <Group id="subagent-claims"
        title={t("session.sub_agent_write_scope")}
        hint={t("session.how_a_background_sub_agent")}
      >
        <SubagentClaims port={port} onChanged={onChanged} />
      </Group>
    </>
  );
}
