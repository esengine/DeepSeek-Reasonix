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
    <Group id="receipt" title={t("common.deliveryReceiptCard")} hint={t("receiptSetting.atTheEndOfEach")}>
      <div className="lrow">
        <span className="tx">
          <span className="lb">{t("receiptSetting.showTheDeliveryReceiptCard")}</span>
          <span className="ds">{t("receiptSetting.offByDefaultTurningIt")}</span>
        </span>
        <Switch data-action="chrome.receipt" on={on} label={t("receiptSetting.showTheDeliveryReceiptCard")} onClick={() => {
          setError("");
          saveReceiptPref(port, !on).catch((e) => setError(reason(e)));
        }} />
      </div>
      {error && <div className="why">{error}</div>}
    </Group>
  );
}
