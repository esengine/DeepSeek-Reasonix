import { t } from "../i18n";
import type { AccountState } from "../port/port";

// signedIn with an error is a token that is still here and an identity service
// that could not be reached — the row says so rather than showing a stranger.
export function AccountRow({ account, unread, onOpen }: { account: AccountState | null; unread?: string; onOpen: () => void }) {
  const user = account?.user;
  const name = user?.label || user?.handle || user?.email || t("common.signedIn");
  const initial = [...name][0]?.toUpperCase();
  const signedIn = !!account?.signedIn;
  const identityError = account?.error ? `${t("common.cannotReachTheIdentityService")}：${account.error}` : "";

  return (
    <button className="acctrow" data-action="chrome.account" onClick={onOpen} aria-label={t("common.account")} title={identityError ? `${signedIn ? name : t("common.notSignedIn")} — ${identityError}` : undefined}>
      <span className="ini" aria-hidden="true" data-off={signedIn ? undefined : ""}>
        <span>{signedIn ? initial : "·"}</span>
      </span>
      <span className="who">
        <span className="n">{signedIn ? name : t("common.notSignedIn")}</span>
        <span className="d">
          {account?.expired
            ? t("accountRow.signInExpired")
            : account?.error
              ? identityError
              : signedIn
                ? user?.email || ""
                : unread
                  ? t("accountRow.signInStatusUnavailable")
                  : t("accountRow.onlyTheNetworkedFeaturesNeed")}
        </span>
      </span>
    </button>
  );
}
