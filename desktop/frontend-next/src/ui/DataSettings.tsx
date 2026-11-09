import { t } from "../i18n";
import type { AgentPort } from "../port/port";
import type { HubPort } from "../port/hub";
import { Group } from "./Group";
import { SectionCard } from "./SectionCard";
import { Memory } from "./Memory";
import { UsageSettings } from "./UsageSettings";
import { Storage } from "./Storage";
import { AutoArchive } from "./AutoArchive";
import { SessionDirSettings } from "./SessionDirSettings";
import { LegacySessionsSection } from "./LegacySessionsSection";
import { BackupFile } from "./BackupFile";
import { Backup } from "./Backup";

interface Props {
  port: AgentPort;
  homePort: AgentPort;
  localPort: AgentPort;
  hub: HubPort;
  workspace: string;
  cwd?: string;
  busy: string;
  signedIn: boolean;
  onIsolate: () => void;
  onChanged: () => void;
  onRecovered: () => void;
}

export function DataSettings({ port, homePort, localPort, hub, workspace, cwd, busy, signedIn, onIsolate, onChanged, onRecovered }: Props) {
  return (
    <>
      <SectionCard title={t("data.memory_and_usage")} hint={t("data.what_the_agent_remembers_and")}>
      <Group id="memory"
      title={t("data.memory")}
      hint={t("settings.what_it_remembers")}
    >
      <Memory port={localPort} />
    </Group>
      <UsageSettings port={homePort} onChanged={onChanged} />
      </SectionCard>
      <SectionCard title={t("data.session_storage")} hint={t("data.where_sessions_and_indexes_live")}>
      <Group id="storage" title={t("common.storage")} hint={t("settings.storage_hint")}><Storage port={homePort} hub={hub} workspace={workspace} onRecovered={onRecovered} /></Group>
      <AutoArchive port={port} />
      <SessionDirSettings cwd={cwd} busy={busy} onIsolate={onIsolate} />
      <LegacySessionsSection />
      </SectionCard>
      <SectionCard title={t("data.backup")} hint={t("data.take_your_configuration_to_another")}>
      <Group id="backup-file" title={t("data.export_and_import_config")} hint={t("settings.config_in_one_file")}><BackupFile port={port} /></Group>
      {signedIn && (
      <Group id="backup" title={t("data.cloud_backup")} hint={t("settings.back_up_to_the_cloud")}>
      <Backup port={port} />
    </Group>
      )}
      </SectionCard>
    </>
  );
}
