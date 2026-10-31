import { useEffect, useState } from "react";
import { t } from "../../i18n";
import { reason } from "../../i18n/kernel";
import type { TeamPort, TeamSecrets, TeamView } from "../../port/team";
import { pushToast } from "../../state/toasts";
import { SecretForm } from "./SecretForm";

// What the person saved for the agents, by name and who may use it — never a
// value — the requests still waiting, each answerable here, and the retired
// values still masked until the person forgets them.
export function SecretList({ team, view }: { team: TeamPort; view: TeamView }) {
  const [book, setBook] = useState<TeamSecrets | null>(null);
  const [confirming, setConfirming] = useState("");
  const [round, setRound] = useState(0);
  const [forgetting, setForgetting] = useState(false);
  useEffect(() => {
    let live = true;
    void team.secrets().then((b) => live && setBook(b), (e) => live && pushToast(reason(e)));
    return () => { live = false; };
  }, [team, view.revision, round]);
  if (!book) return null;
  const agentName = (id: string) => view.agents.find((a) => a.id === id)?.name ?? t("这个 Agent");
  const readOnly = !!view.readOnly;
  const forget = async () => {
    if (!forgetting) {
      setForgetting(true);
      return;
    }
    setForgetting(false);
    try {
      await team.forgetRetiredSecrets();
      setRound((n) => n + 1);
    } catch (e) {
      pushToast(reason(e));
    }
  };
  const retired = [...new Set(book.retired.map((r) => r.name))];
  const remove = async (name: string) => {
    if (confirming !== name) {
      setConfirming(name);
      return;
    }
    setConfirming("");
    try {
      await team.deleteSecret(name);
      setRound((n) => n + 1);
    } catch (e) {
      pushToast(reason(e));
    }
  };
  return (
    <>
      {!readOnly && book.requests.map((q) => (
        <div key={`${q.name}-${q.agent}`} className="team-secret" role="group" aria-label={t("{agent} 需要 {name}", { agent: agentName(q.agent), name: q.name })}>
          <b>{t("{agent} 需要 {name}", { agent: agentName(q.agent), name: q.name })}</b>
          <SecretForm team={team} name={q.name} purpose={q.purpose} agent={q.agent} current={book.secrets.find((s) => s.name === q.name)} guarded={book.guarded} nameOf={agentName} members={view.agents.map((a) => a.id)} onDone={() => setRound((n) => n + 1)} />
        </div>
      ))}
      {book.secrets.length === 0 ? (
        <p className="team-note">{t("还没有保存机密")}</p>
      ) : (
        <ul className="team-shared" aria-label={t("机密")}>
          {book.secrets.map((s) => (
            <li key={s.name}>
              <span className="team-shared-text">
                <b>{s.name}</b>
                <span>{[s.purpose, s.agents?.length ? t("只给 {name}", { name: s.agents.map(agentName).join("、") }) : t("所有 Agent")].filter(Boolean).join(" · ")}</span>
              </span>
              {!readOnly && (
                <button type="button" data-action={confirming === s.name ? "team.secret-delete" : "team.secret-delete-ask"} data-target={s.name}
                  aria-label={confirming === s.name ? t("确认删除 {name}", { name: s.name }) : t("删除 {name}", { name: s.name })} onClick={() => void remove(s.name)}>
                  {confirming === s.name ? t("确认删除") : t("删除")}
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
      {confirming && <p className="team-note">{t("删除后命令不再收到它；值会留在本机，只用来继续遮住已经出现过它的对话。")}</p>}
      {retired.length > 0 && (
        <div className="team-secret-retired">
          <p className="team-note">{t("已停用、仍在遮住：{names}", { names: retired.join("、") })}</p>
          {!readOnly && (
            <button type="button" data-action={forgetting ? "team.secret-forget" : "team.secret-forget-ask"}
              aria-label={forgetting ? t("确认清除停用的值") : t("清除停用的值")} onClick={() => void forget()}>
              {forgetting ? t("确认清除？重启 Reasonix 后，出现过它们的对话不再遮住") : t("清除停用的值")}
            </button>
          )}
        </div>
      )}
    </>
  );
}
