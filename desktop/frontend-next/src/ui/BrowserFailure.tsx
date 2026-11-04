import { t } from "../i18n";
import { StudioIcon } from "./StudioIcon";
import type { BrowserLoadFailure } from "../port/host";

// What a Chromium net error means for someone who can act on it, read from the
// error's name. Electron draws no error page, so without this a failed load is
// a blank view with no reason at all.
function explain(reason: string): string {
  if (reason.startsWith("ERR_CERT_") || reason === "ERR_SSL_PINNED_KEY_NOT_IN_CERT_CHAIN") {
    return t("workbench.the_sites_certificate_is_not");
  }
  if (reason.startsWith("ERR_SSL_") || reason === "ERR_BAD_SSL_CLIENT_AUTH_CERT") {
    return t("workbench.a_secure_connection_could_not");
  }
  if (reason.startsWith("ERR_PROXY_") || reason.startsWith("ERR_TUNNEL_")) {
    return t("workbench.the_system_proxy_could_not");
  }
  switch (reason) {
    case "ERR_NAME_NOT_RESOLVED":
    case "ERR_NAME_RESOLUTION_FAILED":
      return t("workbench.this_address_could_not_be");
    case "ERR_FILE_NOT_FOUND":
      return t("workbench.this_file_could_not_be");
    case "ERR_ACCESS_DENIED":
      return t("workbench.you_do_not_have_permission");
    case "ERR_CONNECTION_REFUSED":
      return t("workbench.the_connection_was_refused_check");
    case "ERR_CONNECTION_TIMED_OUT":
    case "ERR_TIMED_OUT":
    case "ERR_ADDRESS_UNREACHABLE":
      return t("workbench.the_connection_timed_out_or");
    case "ERR_INVALID_AUTH_CREDENTIALS":
      return t("workbench.sign_in_failed_check_the");
    default:
      return t("workbench.the_page_did_not_load");
  }
}

// The system opener is never handed a file: link, so the way out it offers
// would do nothing for one.
function isFile(raw: string): boolean {
  try {
    return new URL(raw).protocol === "file:";
  } catch {
    return false;
  }
}

interface Props {
  failure: BrowserLoadFailure;
  onRetry: () => void;
  onExternal: () => void;
  onTrust: () => void;
}

/** BrowserFailure takes the place of a page that did not load, and says why. */
export function BrowserFailure({ failure, onRetry, onExternal, onTrust }: Props) {
  const cert = failure.certificate;
  return (
    <div className="bfail" role="alert">
      <StudioIcon name="globe" />
      <b>{t("workbench.this_page_could_not_be")}</b>
      <p>{explain(failure.reason)}</p>
      <code>
        {failure.reason || failure.code} · {failure.url}
      </code>
      {/* What the person is about to trust, shown before they trust it: the
          issuer is how a company certificate is told from an impostor's. */}
      {cert && (
        <dl className="bfail-cert">
          <dt>{t("workbench.issued_to")}</dt>
          <dd>{cert.subject}</dd>
          <dt>{t("workbench.issued_by")}</dt>
          <dd>{cert.issuer}</dd>
          <dt>{t("workbench.valid_until")}</dt>
          <dd>{cert.expiry ? new Date(cert.expiry * 1000).toLocaleDateString() : "—"}</dd>
          <dt>{t("common.fingerprint")}</dt>
          <dd className="fp">{cert.fingerprint}</dd>
        </dl>
      )}
      <div className="bfail-acts">
        <button className="btn sm" data-action="browser.control" data-value="retry" onClick={onRetry}>
          {t("common.retry")}
        </button>
        {!isFile(failure.url) && (
          <button className="btn sm" data-action="browser.external" onClick={onExternal}>
            {t("workbench.open_in_external_browser")}
          </button>
        )}
        {cert && (
          <button className="btn sm" data-action="browser.trust-certificate" onClick={onTrust}>
            {t("workbench.proceed_anyway_this_run_only")}
          </button>
        )}
      </div>
      {cert && <p className="bfail-note">{t("workbench.continue_only_if_you_know")}</p>}
    </div>
  );
}
