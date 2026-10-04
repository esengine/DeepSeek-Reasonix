import { t } from "../i18n";
import type { AccountState } from "../port/port";

// signedIn with an error is a token that is still here and an identity service
// that could not be reached — the row says so rather than showing a stranger.
export function AccountRow({ account, unread, onOpen }: { account: AccountState | null; unread?: string; onOpen: () => void }) {
  const user = account?.user;
  const name = user?.label || user?.handle || user?.email || t("已登录");
  const initial = [...name][0]?.toUpperCase();
  const signedIn = !!account?.signedIn;
  const identityError = account?.error ? `${t("无法连接身份服务")}：${account.error}` : "";

  return (
    <button className="acctrow" data-action="chrome.account" onClick={onOpen} aria-label={t("账号")} title={identityError ? `${signedIn ? name : t("未登录")} — ${identityError}` : undefined}>
      <span className="ini" aria-hidden="true" data-off={signedIn ? undefined : ""}>
        {signedIn ? initial : "·"}
      </span>
      <span className="who">
        <span className="n">{signedIn ? name : t("未登录")}</span>
        <span className="d">
          {account?.expired
            ? t("登录已过期")
            : account?.error
              ? identityError
              : signedIn
                ? user?.email || ""
                : unread
                  ? t("登录状态不可用")
                  : t("只有联网功能需要它")}
        </span>
      </span>
    </button>
  );
}
