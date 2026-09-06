import { useRef, useState } from "react";
import { t } from "../i18n";
import type { AccountState, AgentPort } from "../port/port";

// Nothing here gates the agent, so the panel says what an account is for before
// it offers one: a local tool asking to log in reads as "it wants to upload my
// code" unless you answer that first.
//
// state is App's, not this panel's. Fetching its own opened on null and drew
// the signed-out branch for a round trip, which is how a click aimed at 登录
// landed on 退出 when the answer arrived mid-reach.
export function Account({ port, state, reload }: { port: AgentPort; state: AccountState | null; reload: () => void }) {
  const [code, setCode] = useState<{ userCode: string; uri: string } | null>(null);
  const [busy, setBusy] = useState(false);
  const [confirm, setConfirm] = useState(false);
  const [error, setError] = useState("");
  const cancelled = useRef(false);

  const signIn = async () => {
    setError("");
    setBusy(true);
    cancelled.current = false;
    try {
      const grant = await port.accountLogin();
      setCode({ userCode: grant.userCode, uri: grant.verificationUriComplete || grant.verificationUri });
      // The approval page opens in the real browser: a window with no address
      // bar cannot be checked, and the password manager lives there anyway.
      window.open(grant.verificationUriComplete || grant.verificationUri, "_blank", "noopener");
      const deadline = Date.now() + grant.expiresIn * 1000;
      let wait = Math.max(grant.interval, 1) * 1000;
      while (!cancelled.current && Date.now() < deadline) {
        await new Promise((r) => setTimeout(r, wait));
        if (cancelled.current) return;
        const res = await port.accountPoll(grant.deviceCode);
        if (res.status === "complete") {
          setCode(null);
          reload();
          return;
        }
        if (res.slowDown) wait += 5000;
      }
      if (!cancelled.current) setError("这次登录请求已过期，再试一次。");
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
      setCode(null);
    }
  };

  const signOut = async () => {
    // Two steps on purpose: this button appears where 登录 was, and signing out
    // of an account you just signed into is pure loss.
    if (!confirm) {
      setConfirm(true);
      return;
    }
    setConfirm(false);
    setBusy(true);
    try {
      await port.accountLogout();
      reload();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  };

  // Unknown is its own state. Rendering it as signed out puts an action on
  // screen that the next tick may replace with its opposite.
  if (state === null) {
    return <p className="acct-note">{t("正在检查登录状态…")}</p>;
  }

  if (state.signedIn && state.user) {
    return (
      <div className="acct">
        <div className="acct-who">
          <span className="nm">{state.user.label}</span>
          <span className="em">{state.user.email}</span>
        </div>
        {state.error && <p className="acct-note">连不上身份服务：{state.error}</p>}
        <div className="acct-act">
          <button className="btn" data-action="account.sign-out" onClick={signOut} disabled={busy} onMouseLeave={() => setConfirm(false)}>
            {t(confirm ? "确认退出" : "退出登录")}
          </button>
          <span className="acct-note">{t("只清除本地登录凭证，不影响会话、记忆和配置")}</span>
        </div>
      </div>
    );
  }

  return (
    <div className="acct">
      <p className="acct-note">
        {t("用于社区发帖和崩溃问题跟进。与你的模型 API key 无关 —— 登录不会上传你的对话、代码或密钥。")}
      </p>
      {state.expired && <p className="acct-note">{t("上次的登录已过期。")}</p>}
      {code ? (
        <div className="acct-code">
          <span className="cd">{code.userCode}</span>
          <span className="acct-note">
            在浏览器里打开的页面输入这串代码。没自动打开就手动访问 {code.uri}
          </span>
          <button className="btn" onClick={() => (cancelled.current = true)}>
            {t("取消")}
          </button>
        </div>
      ) : (
        <div className="acct-act">
          <button className="btn" data-action="account.sign-in" onClick={signIn} disabled={busy}>
            {t(busy ? "正在打开…" : "登录")}
          </button>
        </div>
      )}
      {error && <p className="acct-note" data-err="">{error}</p>}
    </div>
  );
}
