import { t } from "../i18n";
import type { AgentPort } from "../port/port";
import { Group } from "./Group";
import { SubagentClaims } from "./SubagentClaims";
import { WriteLease } from "./WriteLease";

export function SessionCoordination({ port, onChanged }: { port: AgentPort; onChanged: () => void }) {
  return (
    <>
      <Group id="write-lease"
        title={t("common.severalSessionsWritingOneProject")}
        hint={t("sessionCoordination.howSessionsInOneProject")}
      >
        <WriteLease port={port} />
      </Group>
      <Group id="subagent-claims"
        title={t("common.subAgentWriteScope")}
        hint={t("sessionCoordination.howABackgroundSubAgent")}
      >
        <SubagentClaims port={port} onChanged={onChanged} />
      </Group>
    </>
  );
}
