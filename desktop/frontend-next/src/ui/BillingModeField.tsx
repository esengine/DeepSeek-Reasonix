import { t } from "../i18n";

export const PAYG = "payg";
export const SUBSCRIPTION = "subscription_equivalent";

/** How this connection is billed. A subscription's figures are an API-priced
 *  estimate of the plan, so the usage page keeps them out of what was paid. */
export function BillingModeField({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  return (
    <label className="grow full">
      <span>{t("计费方式")}</span>
      <select aria-label={t("计费方式")} data-action="provider.draft" data-value="billing-mode" value={value} onChange={(e) => onChange(e.target.value)}>
        <option value={PAYG}>{t("按量计费（默认）")}</option>
        <option value={SUBSCRIPTION}>{t("订阅套餐（按价格折算的估算）")}</option>
      </select>
      <i className="tip">{t("订阅套餐的用量按上面的价格折算成估算值，单独显示，不会计入实际花费。")}</i>
    </label>
  );
}
