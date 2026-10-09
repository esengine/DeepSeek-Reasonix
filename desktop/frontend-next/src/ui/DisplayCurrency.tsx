import { useEffect, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort } from "../port/port";
import type { DisplayCurrencyMode, DisplayCurrencySettings } from "../port/boundary";
import { Group } from "./Group";

const MODES: [DisplayCurrencyMode, string][] = [
  ["auto", "自动"],
  ["CNY", "人民币 CNY"],
  ["USD", "美元 USD"],
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
    <Group id="currency" title={t("费用显示币种")}
      hint={t("只决定费用以哪种货币显示，不改动各来源的价目表。「自动」跟随账户余额的币种，没有余额信息时按价目表的币种显示。")}>
      {!state ? (
        <div className="empty">{t("无法读取币种设置。")}</div>
      ) : (
        <div className="box">
          <div className="lrow">
            <span className="tx">
              <span className="lb">{t("显示币种")}</span>
              <span className="ds">{t("当前会话的费用、余额与用量页立即按所选币种显示，无需重启。")}</span>
            </span>
            <div className="seg" data-text role="group" aria-label={t("显示币种")}>
              {MODES.map(([m, label]) => (
                <button key={m} data-action="currency.mode" data-value={m} aria-pressed={state.mode === m} disabled={busy} onClick={() => void pick(m)}>
                  {t(label)}
                </button>
              ))}
            </div>
          </div>
          {error && <div className="why">{error}</div>}
        </div>
      )}
    </Group>
  );
}
