import { useState } from "react";
import { t } from "../i18n";
import type { AgentPort } from "../port/port";
import { DisplayCurrency } from "./DisplayCurrency";
import { Group } from "./Group";
import { UploadUsage } from "./UploadUsage";
import { Usage } from "./Usage";

// The report is keyed on the currency choice: its amounts are read once, so a
// new currency has to bring a new read rather than relabel the old figures.
export function UsageSettings({ port, onChanged }: { port: AgentPort; onChanged: () => void }) {
  const [rev, setRev] = useState(0);
  return (
    <>
      <Group id="usage" pill={t("settings.view_usage")}
        title={t("usage.usage_cost")}
        hint={t("settings.usage_hint")}
      >
        <Usage key={rev} port={port} />
      </Group>
      <UploadUsage port={port} />
      <DisplayCurrency port={port} onChanged={() => { setRev((n) => n + 1); onChanged(); }} />
    </>
  );
}
