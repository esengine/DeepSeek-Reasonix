import { useEffect, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort } from "../port/port";
import type { DisplayCurrencyMode, DisplayCurrencySettings } from "../port/boundary";
import { Group } from "./Group";

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

  return (
    <Group id="currency" title={t("usage.cost_display_currency")}
      hint={t("usage.only_decides_which_currency_costs")}>
      {!state ? (
        <div className="empty">{t("usage.could_not_read_the_currency")}</div>
      ) : (
        <div className="box">
          <div className="lrow">
            <span className="tx">
              <span className="lb">{t("usage.display_currency")}</span>
              <span className="ds">{t("usage.the_sessions_cost_the_balance")}</span>
            </span>
            <div className="seg" data-text role="group" aria-label={t("usage.display_currency")}>
              {MODES.map(([m, label]) => (
                <button key={m} data-action="currency.mode" data-value={m} aria-pressed={state.mode === m} disabled={busy} onClick={() => void pick(m)}>
                  {t(label)}
                </button>
              ))}
            </div>
          </div>
          {state.mode !== "auto" && state.unconverted.length > 0 && (
            <div className="why">
              {t("usage.these_models_prices_are_not", {
                models: state.unconverted.map((u) => t("{model}（{currency}）", { model: u.model, currency: u.currency })).join(t("、")),
              })}
            </div>
          )}
          {error && <div className="why">{error}</div>}
        </div>
      )}
    </Group>
  );
}
