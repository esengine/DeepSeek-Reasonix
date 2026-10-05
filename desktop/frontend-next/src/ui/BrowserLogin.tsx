import { useEffect, useRef, useState } from "react";
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
    <div className="askveil" role="dialog" aria-modal="true" aria-labelledby="blogin-t">
      <div className="askcard">
        <h2 id="blogin-t">{ask.proxy ? t("代理 {host} 需要登录", { host: where }) : t("{host} 需要登录", { host: where })}</h2>
        <p className="h">
          {ask.realm ? t("站点提示：{realm}。", { realm: ask.realm }) : ""}
          {t("用户名和密码只交给这个网页，不会保存，也不会发给 Reasonix 或模型。")}
        </p>
        <input
          ref={first}
          data-action-keydown="browser.login"
          value={username}
          autoComplete="username"
          placeholder={t("用户名")}
          aria-label={t("用户名")}
          onChange={(e) => setUsername(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && submit()}
        />
        <input
          data-action-keydown="browser.login"
          type="password"
          value={password}
          autoComplete="current-password"
          placeholder={t("密码")}
          aria-label={t("密码")}
          onChange={(e) => setPassword(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && submit()}
        />
        <div className="askact">
          <button data-action="browser.login" data-value="cancel" onClick={() => onAnswer("", "")}>
            {t("取消")}
          </button>
          <button data-action="browser.login" data-value="submit" data-go="" disabled={!username} onClick={submit}>
            {t("登录")}
          </button>
        </div>
      </div>
    </div>
  );
}
