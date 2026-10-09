import { useState } from "react";
import { t } from "../i18n";
import type { AgentPort } from "../port/port";
import { DisplayCurrency } from "./DisplayCurrency";
import { Group } from "./Group";
import { Usage } from "./Usage";

// The report is keyed on the currency choice: its amounts are read once, so a
// new currency has to bring a new read rather than relabel the old figures.
export function UsageSettings({ port, onChanged }: { port: AgentPort; onChanged: () => void }) {
  const [rev, setRev] = useState(0);
  return (
    <>
      <DisplayCurrency port={port} onChanged={() => { setRev((n) => n + 1); onChanged(); }} />
      <Group id="usage"
        title={t("用量与成本")}
        hint={t("本机记录的 token 用量与花费，仅保存在这台机器上，不会上传。命中缓存的输入按缓存价计费，因此命中率直接影响费用。")}
      >
        <Usage key={rev} port={port} />
      </Group>
    </>
  );
}
