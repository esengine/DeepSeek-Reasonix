import { t } from "../i18n";
import type { HubPort } from "../port/hub";
import type { AgentPort } from "../port/port";
import { remoteDeviceId } from "../port/cloud_remote";
import { Group } from "./Group";
import { Network } from "./Network";
import { PhoneAccess } from "./PhoneAccess";
import { PhoneNotifications } from "./PhoneNotifications";
import { PhoneReminders } from "./PhoneReminders";
import { Remotes } from "./Remotes";
import { SectionCard } from "./SectionCard";

interface Props {
  port: AgentPort;
  networkPort: AgentPort;
  host: string;
  hub: HubPort;
  onError: (e: unknown) => void;
  remote: boolean;
}

export function ConnectSettings({ port, networkPort, host, hub, onError, remote }: Props) {
  return (
    <>
      <SectionCard title={t("common.network")}>
        <Group id="network" pill={t("safety.details")} title={t("common.network")} hint={t("settings.network_hint")}>
          <Network port={networkPort} host={host} />
        </Group>
      </SectionCard>
      {remote && (
        <SectionCard title={t("settings.other_machines")}>
          <Group id="remote" pill={t("settings.remote_machines")} title={t("settings.remote")} hint={t("settings.remote_hint")}>
            <Remotes hub={hub} onError={onError} />
          </Group>
          <Group id="phone" pill={t("settings.set_up_phone_access")} title={t("remote.phone_access")} hint={t("settings.phone_hint")}>
            <PhoneAccess hub={hub} onError={onError} />
            <PhoneNotifications hub={hub} port={port} onError={onError} />
            {remoteDeviceId() && <PhoneReminders host={remoteDeviceId()!} />}
          </Group>
        </SectionCard>
      )}
    </>
  );
}
