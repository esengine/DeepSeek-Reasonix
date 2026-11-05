import { useEffect, useState, type ComponentProps } from "react";
import { t } from "../i18n";
import type { AgentPort, CompactionSettings } from "../port/port";
import { Compaction } from "./Compaction";
import { ModelUsage } from "./ModelUsage";
import { ProviderHealthSetting } from "./ProviderHealthSetting";
import { Providers, groupAccounts } from "./Providers";
import { SectionCard } from "./SectionCard";
import { SettingDeep } from "./SettingDeep";

interface Props {
  port: AgentPort;
  usage: ComponentProps<typeof ModelUsage>;
  providers: ComponentProps<typeof Providers>;
  onChanged: () => void;
  onFailed: (why: string) => void;
}

export function ModelSettings({ port, usage, providers, onChanged, onFailed }: Props) {
  const [services, setServices] = useState<number | null>(null);
  const [compaction, setCompaction] = useState<CompactionSettings | null>(null);
  useEffect(() => {
    port.providers().then((list) => setServices(groupAccounts(list).length)).catch(() => setServices(null));
    port.compaction().then(setCompaction).catch(() => setCompaction(null));
  }, [port]);
  const share = compaction && compaction.context_window > 0 ? Math.round(compaction.ratio * 100) : null;
  return (
    <>
      <SectionCard title={t("models.services")}>
        <SettingDeep id="providers" title={t("common.model_services")} act={t("models.manage_services")}
          hint={services === null ? undefined : t("models.services_connected_n", { n: services })}>
          <section className="prefs-card"><Providers {...providers} /></section>
        </SettingDeep>
        <ProviderHealthSetting port={port} onChanged={onChanged} onFailed={onFailed} />
      </SectionCard>
      <SectionCard title={t("models.uses_and_context")}>
        <SettingDeep id="model" title={t("settings.models_by_use")} act={t("models.set_by_use")}
          hint={t("models.which_model_for_each_use")}>
          <section className="prefs-card"><ModelUsage {...usage} /></section>
        </SettingDeep>
        <SettingDeep id="context" title={t("settings.context_maintenance")} act={share === null ? "—" : `${share}%`}
          hint={share === null ? undefined : t("models.compacts_from_this_share", { p: share })}>
          <section className="prefs-card"><Compaction port={port} /></section>
        </SettingDeep>
      </SectionCard>
    </>
  );
}
