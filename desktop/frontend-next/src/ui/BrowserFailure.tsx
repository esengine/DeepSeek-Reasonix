import { t } from "../i18n";
import type { BrowserLoadFailure } from "../port/host";

// What a Chromium net error means for someone who can act on it, read from the
// error's name. Electron draws no error page, so without this a failed load is
// a blank view with no reason at all.
function explain(reason: string): string {
  if (reason.startsWith("ERR_CERT_") || reason === "ERR_SSL_PINNED_KEY_NOT_IN_CERT_CHAIN") {
    return t("browserFailure.theSiteSCertificateIs");
  }
  if (reason.startsWith("ERR_SSL_") || reason === "ERR_BAD_SSL_CLIENT_AUTH_CERT") {
    return t("browserFailure.aSecureConnectionCouldNot");
  }
  if (reason.startsWith("ERR_PROXY_") || reason.startsWith("ERR_TUNNEL_")) {
    return t("browserFailure.theSystemProxyCouldNot");
  }
  switch (reason) {
    case "ERR_NAME_NOT_RESOLVED":
    case "ERR_NAME_RESOLUTION_FAILED":
      return t("browserFailure.thisAddressCouldNotBe");
    case "ERR_FILE_NOT_FOUND":
      return t("browserFailure.thisFileCouldNotBe");
    case "ERR_ACCESS_DENIED":
      return t("browserFailure.youDoNotHavePermission");
    case "ERR_CONNECTION_REFUSED":
      return t("browserFailure.theConnectionWasRefusedCheck");
    case "ERR_CONNECTION_TIMED_OUT":
    case "ERR_TIMED_OUT":
    case "ERR_ADDRESS_UNREACHABLE":
      return t("browserFailure.theConnectionTimedOutOr");
    case "ERR_INVALID_AUTH_CREDENTIALS":
      return t("browserFailure.signInFailedCheckThe");
    default:
      return t("browserFailure.thePageDidNotLoad");
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
      <b>{t("browserFailure.thisPageCouldNotBe")}</b>
      <p>{explain(failure.reason)}</p>
      <code>
        {failure.reason || failure.code} · {failure.url}
      </code>
      {/* What the person is about to trust, shown before they trust it: the
          issuer is how a company certificate is told from an impostor's. */}
      {cert && (
        <dl className="bfail-cert">
          <dt>{t("browserFailure.issuedTo")}</dt>
          <dd>{cert.subject}</dd>
          <dt>{t("browserFailure.issuedBy")}</dt>
          <dd>{cert.issuer}</dd>
          <dt>{t("browserFailure.validUntil")}</dt>
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
            {t("common.openInExternalBrowser")}
          </button>
        )}
        {cert && (
          <button className="btn sm" data-action="browser.trust-certificate" onClick={onTrust}>
            {t("browserFailure.proceedAnywayThisRunOnly")}
          </button>
        )}
      </div>
      {cert && <p className="bfail-note">{t("browserFailure.continueOnlyIfYouKnow")}</p>}
    </div>
  );
}
