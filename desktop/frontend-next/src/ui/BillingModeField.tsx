import { t } from "../i18n";

export const PAYG = "payg";
export const SUBSCRIPTION = "subscription_equivalent";

/** How this connection is billed. A subscription's figures are an API-priced
 *  estimate of the plan, so the usage page keeps them out of what was paid. */
export function BillingModeField({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  return (
    <label className="grow full">
      <span>{t("usage.billing")}</span>
      <select aria-label={t("usage.billing")} data-action="provider.draft" data-value="billing-mode" value={value} onChange={(e) => onChange(e.target.value)}>
        <option value={PAYG}>{t("usage.pay_as_you_go_default")}</option>
        <option value={SUBSCRIPTION}>{t("usage.subscription_plan_estimated_at_the")}</option>
      </select>
      <i className="tip">{t("usage.a_plans_usage_is_converted")}</i>
    </label>
  );
}
