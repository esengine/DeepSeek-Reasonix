import { t } from "../i18n";

export const PAYG = "payg";
export const SUBSCRIPTION = "subscription_equivalent";

/** How this connection is billed. A subscription's figures are an API-priced
 *  estimate of the plan, so the usage page keeps them out of what was paid. */
export function BillingModeField({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  return (
    <label className="grow full">
      <span>{t("billingModeField.billing")}</span>
      <select aria-label={t("billingModeField.billing")} data-action="provider.draft" data-value="billing-mode" value={value} onChange={(e) => onChange(e.target.value)}>
        <option value={PAYG}>{t("billingModeField.payAsYouGoDefault")}</option>
        <option value={SUBSCRIPTION}>{t("billingModeField.subscriptionPlanEstimatedAtThe")}</option>
      </select>
      <i className="tip">{t("billingModeField.aPlanSUsageIs")}</i>
    </label>
  );
}
