import { useEffect, useState } from "react";
import { t } from "../../i18n";
import type { Tool } from "../../port/wire";
import type { TeamSecrets } from "../../port/team";
import { useCurrentTeam } from "../../state/team";
import { SecretForm } from "./SecretForm";
import "../../styles/team.css";

// The kernel's own spelling of a secret name: upper case, anything else an
// underscore, trimmed of underscores.
function secretName(raw: string): string {
  return raw.trim().toUpperCase().replace(/[^A-Z0-9]/g, "_").replace(/^_+|_+$/g, "").slice(0, 40);
}

function nameOf(tool: Tool): string {
  try {
    return secretName(String((JSON.parse(tool.args ?? "{}") as { name?: unknown }).name ?? ""));
  } catch {
    return "";
  }
}

// What an agent's request_secret call shows: the card the person fills in
// while the request is open, then what became of it. Every state is read
// from the kernel's book, which never holds a value the window can see.
export function SecretCard({ tool, running }: { tool: Tool; running: boolean }) {
  const { view, team } = useCurrentTeam();
  const [book, setBook] = useState<TeamSecrets | null>(null);
  const [outcome, setOutcome] = useState<"saved" | "declined" | null>(null);
  const name = nameOf(tool);
  useEffect(() => {
    if (!team || running) return;
    let live = true;
    void team.secrets().then((b) => live && setBook(b), () => {});
    return () => { live = false; };
  }, [team, running, view?.revision, outcome]);
  if (running || !name) return <div className="team-secret" role="status">{t("正在请求机密…")}</div>;
  if (!team || !book) return null;
  const request = book.requests.find((q) => q.name === name);
  const saved = book.secrets.find((s) => s.name === name);
  const agentName = (id?: string) => view?.agents.find((a) => a.id === id)?.name ?? t("这个 Agent");
  if (request && !outcome) {
    return (
      <div className="team-secret" role="group" aria-label={t("{agent} 需要 {name}", { agent: agentName(request.agent), name })}>
        <b>{t("{agent} 需要 {name}", { agent: agentName(request.agent), name })}</b>
        <SecretForm team={team} name={name} purpose={request.purpose} agent={request.agent} agentName={agentName(request.agent)} agents={view?.agents} onDone={setOutcome} />
      </div>
    );
  }
  if (saved && outcome !== "declined") {
    const scope = saved.agents?.length ? t("只给 {name}", { name: saved.agents.map((a) => agentName(a)).join("、") }) : t("尚未授权任何成员");
    return <div className="team-secret" role="group" aria-label={name} data-state="saved"><span>{t("已保存 {name} · {scope}", { name, scope })}</span></div>;
  }
  return <div className="team-secret" role="group" aria-label={name} data-state="declined"><span>{t("已拒绝提供 {name}", { name })}</span></div>;
}
