import { useRef, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AccountState, AgentPort } from "../port/port";

// App owns the account read so the panel and sidebar show the same state.
export function Account({ port, state, unread, reload }: { port: AgentPort; state: AccountState | null; unread?: string; reload: () => void }) {
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
      const uri = grant.verificationUriComplete || grant.verificationUri;
      setCode({ userCode: grant.userCode, uri });
      // The approval page opens in the real browser: a window with no address
      // bar cannot be checked, and the password manager lives there anyway.
      await port.openExternal(uri);
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
      if (!cancelled.current) setError(t("settings.this_sign_in_request_expired"));
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy(false);
      setCode(null);
    }
  };

  const signOut = async () => {
    // Confirmation protects a click aimed at sign-in before state refreshes.
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
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  };

  // Unknown is its own state. Rendering it as signed out puts an action on
  // screen that the next tick may replace with its opposite. Refused is a
  // third: the kernel already said why, and waiting is not what to do about it.
  if (unread) {
    return (
      <div className="find" data-lvl="warn" role="alert">
        <span className="t">{t("settings.sign_in_status_could_not")}</span>
        <span className="why">
          {unread}
          <button className="lnk" data-action="account.reload" onClick={reload}>
            {t("common.retry")}
          </button>
        </span>
      </div>
    );
  }

  if (state === null) {
    return <p className="acct-note">{t("settings.checking_sign_in_status")}</p>;
  }

  if (state.signedIn) {
    return (
      <div className="acct">
        <div className="acct-who">
          <span className="nm">{state.user?.label || state.user?.handle || state.user?.email || t("settings.signed_in")}</span>
          {state.user?.email && <span className="em">{state.user.email}</span>}
        </div>
        {state.error && (
          <p className="acct-note" role="alert">
            {t("settings.cannot_reach_the_identity_service")}：{state.error}{" "}
            <button className="lnk" data-action="account.reload" onClick={reload} disabled={busy}>
              {t("common.retry")}
            </button>
          </p>
        )}
        <div className="acct-act">
          <button className="btn" data-action="account.sign-out" onClick={signOut} disabled={busy} onMouseLeave={() => setConfirm(false)}>
            {t(confirm ? "settings.confirm_sign_out" : "settings.sign_out")}
          </button>
          <span className="acct-note">{t("settings.clears_the_local_credential_only")}</span>
        </div>
        {error && <p className="acct-note" data-err="">{error}</p>}
      </div>
    );
  }

  return (
    <div className="acct">
      <p className="acct-note">
        {t("settings.used_for_posting_to_the")}
      </p>
      {state.expired && <p className="acct-note">{t("settings.the_previous_sign_in_has")}</p>}
      {code ? (
        <div className="acct-code">
          <span className="cd">{code.userCode}</span>
          <span className="acct-note">
            {t("settings.type_this_code_into_the", { uri: code.uri })}
          </span>
          <button className="btn" onClick={() => (cancelled.current = true)}>
            {t("common.cancel")}
          </button>
        </div>
      ) : (
        <div className="acct-act">
          <button className="btn" data-action="account.sign-in" onClick={signIn} disabled={busy}>
            {t(busy ? "settings.opening" : "common.sign_in")}
          </button>
        </div>
      )}
      {error && <p className="acct-note" data-err="">{error}</p>}
    </div>
  );
}
