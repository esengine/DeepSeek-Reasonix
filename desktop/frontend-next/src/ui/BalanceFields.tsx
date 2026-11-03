import { t } from "../i18n";
import type { ProviderEntry } from "../port/port";

const FORMAT_LABEL: Record<string, string> = {
  deepseek: "balanceFields.deepSeekFormat",
  moonshot: "balanceFields.moonshotFormat",
  openrouter: "balanceFields.openRouterFormat",
};

// The wallet endpoint and how it answers. The host table that implies a format
// lives in the kernel, so the entry says what its saved address implies and this
// form never guesses from a typed one.
export function BalanceFields({ entry, url, format, onUrl, onFormat }: {
  entry: ProviderEntry;
  url: string;
  format: string;
  onUrl: (v: string) => void;
  onFormat: (v: string) => void;
}) {
  const formats = entry.balanceFormats ?? [];
  const saved = url.trim() === (entry.balanceUrl ?? "").trim();
  const implied = saved ? entry.impliedBalanceFormat ?? "" : "";
  const needs = url.trim() !== "" && format === "" && saved && implied === "";
  return (
    <>
      <label className="grow full">
        <span>{t("balanceFields.balanceAddress")}</span>
        <input
          aria-label={t("balanceFields.balanceAddress")}
          data-action="provider.draft"
          data-value="balance-url"
          value={url}
          spellCheck={false}
          placeholder="https://api.deepseek.com/user/balance"
          onChange={(e) => onUrl(e.target.value)}
        />
        <i className="tip">{t("balanceFields.leaveEmptyToShowNo")}</i>
      </label>
      {url.trim() !== "" && (
        <label className="grow full">
          <span>{t("balanceFields.balanceFormat")}</span>
          <select aria-label={t("balanceFields.balanceFormat")} data-action="provider.draft" data-value="balance-format" value={format} onChange={(e) => onFormat(e.target.value)}>
            <option value="">{implied ? t("balanceFields.fromTheAddress", { format: t(FORMAT_LABEL[implied] ?? implied) }) : t("common.noSelection")}</option>
            {formats.map((f) => (
              <option key={f} value={f}>{t(FORMAT_LABEL[f] ?? f)}</option>
            ))}
          </select>
          <i className="tip">{t("balanceFields.aVendorSOwnDomain")}</i>
        </label>
      )}
      {needs && <div className="why">{t("balanceFields.balanceFormatNotSetThis")}</div>}
    </>
  );
}
