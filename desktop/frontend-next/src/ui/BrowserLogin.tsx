import { useEffect, useRef, useState } from "react";
import { useModalFocus } from "./useModalFocus";
import { t } from "../i18n";
import { host, type BrowserLogin as Ask } from "../port/host";
import { listenAction } from "./listen";

// A page or a proxy in the built-in browser asking for a login. Without an
// answer Electron cancels it, which is how an intranet site behind Basic or
// NTLM authentication reads as "cannot be opened". What is typed goes to the
// window's main process only; the kernel and the agent never see it.
export function BrowserLogin() {
  const [queue, setQueue] = useState<Ask[]>([]);
  const ask = queue[0];

  useEffect(() => host().onBrowserLogin((next) => setQueue((q) => [...q, next])), []);

  const answer = (username: string, password: string) => {
    if (!ask) return;
    host().answerBrowserLogin(ask.id, username, password);
    setQueue((q) => q.slice(1));
  };

  return ask ? <LoginCard key={ask.id} ask={ask} onAnswer={answer} /> : null;
}

function LoginCard({ ask, onAnswer }: { ask: Ask; onAnswer: (username: string, password: string) => void }) {
  const sheet = useRef<HTMLDivElement>(null);
  useModalFocus(sheet);
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const first = useRef<HTMLInputElement>(null);

  useEffect(() => first.current?.focus(), []);

  // Escape cancels, the same answer the button gives: a prompt closed with no
  // answer would leave the page waiting on it.
  useEffect(() => {
    const onKey = (e: Event) => {
      if ((e as KeyboardEvent).key === "Escape") onAnswer("", "");
    };
    return listenAction(window, "keydown", { action: "browser.login", value: "cancel", listener: onKey });
  }, [onAnswer]);

  const where = ask.port ? `${ask.host}:${ask.port}` : ask.host;
  const submit = () => username && onAnswer(username, password);
  return (
    <div ref={sheet} className="askveil" role="dialog" aria-modal="true" aria-labelledby="blogin-t">
      <div className="askcard">
        <h2 id="blogin-t">{ask.proxy ? t("browserLogin.theProxyAsksYouTo", { host: where }) : t("browserLogin.asksYouToSignIn", { host: where })}</h2>
        <p className="h">
          {ask.realm ? t("browserLogin.theSiteSays", { realm: ask.realm }) : ""}
          {t("browserLogin.theUsernameAndPasswordGo")}
        </p>
        <input
          ref={first}
          data-action-keydown="browser.login"
          value={username}
          autoComplete="username"
          placeholder={t("browserLogin.username")}
          aria-label={t("browserLogin.username")}
          onChange={(e) => setUsername(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && submit()}
        />
        <input
          data-action-keydown="browser.login"
          type="password"
          value={password}
          autoComplete="current-password"
          placeholder={t("browserLogin.password")}
          aria-label={t("browserLogin.password")}
          onChange={(e) => setPassword(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && submit()}
        />
        <div className="askact">
          <button data-action="browser.login" data-value="cancel" onClick={() => onAnswer("", "")}>
            {t("common.cancel")}
          </button>
          <button data-action="browser.login" data-value="submit" data-go="" disabled={!username} onClick={submit}>
            {t("common.signIn")}
          </button>
        </div>
      </div>
    </div>
  );
}
