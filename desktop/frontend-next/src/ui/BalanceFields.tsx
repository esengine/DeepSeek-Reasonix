import { t } from "../i18n";
import type { ProviderEntry } from "../port/port";

const FORMAT_LABEL: Record<string, string> = {
  deepseek: "usage.deepseek_format",
  moonshot: "usage.moonshot_format",
  openrouter: "usage.openrouter_format",
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
        <span>{t("usage.balance_address")}</span>
        <input
          aria-label={t("usage.balance_address")}
          data-action="provider.draft"
          data-value="balance-url"
          value={url}
          spellCheck={false}
          placeholder="https://api.deepseek.com/user/balance"
          onChange={(e) => onUrl(e.target.value)}
        />
        <i className="tip">{t("usage.leave_empty_to_show_no")}</i>
      </label>
      {url.trim() !== "" && (
        <label className="grow full">
          <span>{t("usage.balance_format")}</span>
          <select aria-label={t("usage.balance_format")} data-action="provider.draft" data-value="balance-format" value={format} onChange={(e) => onFormat(e.target.value)}>
            <option value="">{implied ? t("usage.from_the_address_format", { format: t(FORMAT_LABEL[implied] ?? implied) }) : t("common.no_selection")}</option>
            {formats.map((f) => (
              <option key={f} value={f}>{t(FORMAT_LABEL[f] ?? f)}</option>
            ))}
          </select>
          <i className="tip">{t("usage.a_vendors_own_domain_is")}</i>
        </label>
      )}
      {needs && <div className="why">{t("usage.balance_format_not_set_this")}</div>}
    </>
  );
}
