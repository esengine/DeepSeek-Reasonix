import { t } from "../i18n";
import type { AccountState } from "../port/port";

// signedIn with an error is a token that is still here and an identity service
// that could not be reached — the row says so rather than showing a stranger.
export function AccountRow({ account, onOpen }: { account: AccountState | null; onOpen: () => void }) {
  const user = account?.user;
  const name = user?.label || user?.handle || user?.email || "";
  const initial = [...(name || "?")][0]?.toUpperCase() ?? "?";
  const signedIn = !!account?.signedIn;

  return (
    <button className="acctrow" data-action="chrome.account" onClick={onOpen} aria-label={t("账号")}>
      <span className="ini" aria-hidden="true" data-off={signedIn ? undefined : ""}>
        {signedIn ? initial : "·"}
      </span>
      <span className="who">
        <span className="n">{signedIn ? name || t("已登录") : t("未登录")}</span>
        <span className="d">
          {account?.expired ? t("登录已过期") : account?.error ? t("连不上身份服务") : signedIn ? user?.email || "" : t("只有联网功能需要它")}
        </span>
      </span>
    </button>
  );
}
