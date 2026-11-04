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
      <SectionCard title={t("记忆与用量")} hint={t("agent 记住了什么、花了多少")}>
      <Group id="memory"
      title={t("记忆")}
      hint={t("agent 自动记录的内容：未经配置，但会据此执行。可以按类型和星系查找，刚记住的在「新的」里等你确认。")}
    >
      <Memory port={localPort} />
    </Group>
      <UsageSettings port={homePort} onChanged={onChanged} />
      </SectionCard>
      <SectionCard title={t("会话存储")} hint={t("会话和索引放在哪里、占多少空间")}>
      <Group id="storage" title={t("存储")} hint={t("数据的存储位置与占用空间。会话和索引会持续增长，配置和凭据不会，因此只有前者可以迁移。迁移在重启后生效。")}><Storage port={homePort} hub={hub} workspace={workspace} onRecovered={onRecovered} /></Group>
      <AutoArchive port={port} />
      <SessionDirSettings cwd={cwd} busy={busy} onIsolate={onIsolate} />
      <LegacySessionsSection />
      </SectionCard>
      <SectionCard title={t("备份")} hint={t("换机器时带走配置")}>
      <Group id="backup-file" title={t("导出与导入配置")} hint={t("不用账号：把本机的模型、扩展、记忆等配置加密导出成一个文件，换机器时再导入。")}><BackupFile port={port} /></Group>
      {signedIn && (
      <Group id="backup" title={t("云备份")} hint={t("把本机的模型、扩展、记忆等配置加密保存到账号，换机器或重装后按项恢复。")}>
      <Backup port={port} />
    </Group>
      )}
      </SectionCard>
    </>
  );
}
