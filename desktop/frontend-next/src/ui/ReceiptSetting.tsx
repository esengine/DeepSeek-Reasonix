import { useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort } from "../port/port";
import { useShowsReceipt } from "../state/foldpref";
import { receiptPrefOf, saveReceiptPref } from "../state/receiptpref";
import { Group } from "./Group";
import { Switch } from "./Switch";

export function ReceiptSetting({ port }: { port: AgentPort }) {
  const on = useShowsReceipt(receiptPrefOf(port));
  const [error, setError] = useState("");
  return (
    <Group id="receipt" title={t("settings.delivery_receipt_card")} hint={t("settings.at_the_end_of_each")}>
      <div className="lrow">
        <span className="tx">
          <span className="lb">{t("settings.show_the_delivery_receipt_card")}</span>
          <span className="ds">{t("settings.off_by_default_turning_it")}</span>
        </span>
        <Switch data-action="chrome.receipt" on={on} label={t("settings.show_the_delivery_receipt_card")} onClick={() => {
          setError("");
          saveReceiptPref(port, !on).catch((e) => setError(reason(e)));
        }} />
      </div>
      {error && <div className="why">{error}</div>}
    </Group>
  );
}
