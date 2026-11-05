import { useEffect, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort } from "../port/port";
import type { DisplayCurrencyMode, DisplayCurrencySettings } from "../port/boundary";
import { SegChoice, SettingRow } from "./SettingRow";

const MODES: [DisplayCurrencyMode, string][] = [
  ["auto", "common.auto"],
  ["CNY", "usage.chinese_yuan_cny"],
  ["USD", "usage.us_dollar_usd"],
];

export function DisplayCurrency({ port, onChanged }: { port: AgentPort; onChanged: () => void }) {
  const [state, setState] = useState<DisplayCurrencySettings | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    port.displayCurrency().then(setState).catch(() => setState(null));
  }, [port]);

  const pick = async (mode: DisplayCurrencyMode) => {
    if (!state || mode === state.mode) return;
    setBusy(true);
    setError("");
    try {
      setState(await port.saveDisplayCurrency(mode));
      onChanged();
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  };

  if (!state) return <div className="empty">{t("usage.could_not_read_the_currency")}</div>;
  return (
    <>
      <SettingRow id="currency" title={t("usage.cost_display_currency")} desc={t("settings.which_currency_costs_show")}>
        <SegChoice<DisplayCurrencyMode> label={t("usage.display_currency")} value={state.mode} action="currency.mode" disabled={busy}
          onPick={(m) => void pick(m)} options={MODES.map(([m, label]) => [m, t(label)] as const)} />
      </SettingRow>
      {state.mode !== "auto" && state.unconverted.length > 0 && (
        <div className="why">
          {t("usage.these_models_prices_are_not", {
            models: state.unconverted.map((u) => t("{model}（{currency}）", { model: u.model, currency: u.currency })).join(t("、")),
          })}
        </div>
      )}
      {error && <div className="why">{error}</div>}
    </>
  );
}
