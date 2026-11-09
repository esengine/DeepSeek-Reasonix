import type { ComponentProps } from "react";
import { t } from "../i18n";
import type { AgentPort } from "../port/port";
import { Compaction } from "./Compaction";
import { Group } from "./Group";
import { ModelUsage } from "./ModelUsage";
import { ProviderHealthSetting } from "./ProviderHealthSetting";
import { Providers } from "./Providers";
import { SectionCard } from "./SectionCard";

interface Props {
  port: AgentPort;
  usage: ComponentProps<typeof ModelUsage>;
  providers: ComponentProps<typeof Providers>;
  onChanged: () => void;
  onFailed: (why: string) => void;
}

export function ModelSettings({ port, usage, providers, onChanged, onFailed }: Props) {
  return (
    <>
      <SectionCard title={t("models.services")}>
        <Group id="providers" title={t("common.model_services")}>
          <Providers {...providers} />
        </Group>
        <ProviderHealthSetting port={port} onChanged={onChanged} onFailed={onFailed} />
      </SectionCard>
      <SectionCard title={t("models.uses_and_context")}>
        <Group id="model" title={t("settings.models_by_use")} hint={t("models.which_model_for_each_use")}>
          <ModelUsage {...usage} />
        </Group>
        <Group id="context" title={t("settings.context_maintenance")}>
          <Compaction port={port} />
        </Group>
      </SectionCard>
    </>
  );
}
