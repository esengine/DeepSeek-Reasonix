import { useEffect, useState } from "react";
import { t } from "../../i18n";
import { reason } from "../../i18n/kernel";
import type { RuntimeView } from "../../port/hub";
import type { ModelEntry } from "../../port/model";
import type { AgentPort } from "../../port/port";
import { useCurrentTeam } from "../../state/team";
import { pushToast } from "../../state/toasts";
import { AgentAvatar } from "./AgentAvatar";

const MAX_CHIPS = 8;

// Written by the host, not the model: the greeting costs nothing, needs no
// provider to be set up, and never becomes a message in a conversation whose
// prefix the cache holds.
const STARTERS = ["介绍一下你能做什么", "帮我组建一个写作小组：一个写手和一个审稿人", "建一个群聊，让成员们一起讨论下周的计划"];

function greeting(name: string, role: string | undefined, main: boolean): string {
  if (main) return t("你好，我是 {name}。我可以按你的要求创建其他 Agent 和群聊，并把工作分派给它们。", { name });
  return role ? t("你好，我是 {name}，{role}。有什么需要我做的？", { name, role }) : t("你好，我是 {name}。有什么需要我做的？", { name });
}

// What an agent's empty conversation shows: the agent saying who it is, the
// models it can run on with the one in use marked, and for a main agent a few
// first requests that fill the composer rather than send.
export function AgentHero({ port, seat, model, onModel, onStarter }: {
  port: AgentPort;
  seat: NonNullable<RuntimeView["team"]>;
  model?: string;
  onModel: () => void;
  onStarter: (text: string) => void;
}) {
  const { view, team } = useCurrentTeam();
  const [models, setModels] = useState<ModelEntry[]>([]);
  const [picked, setPicked] = useState(model);
  useEffect(() => setPicked(model), [model]);
  useEffect(() => {
    if (seat.group) return;
    let live = true;
    void port.models("chat").then((all) => live && setModels(all.filter((m) => !m.notAdded && (m.answers ?? "chat") === "chat")), () => {});
    return () => { live = false; };
  }, [port, seat.group]);
  const agent = view?.agents.find((a) => a.id === seat.agent);
  const name = agent?.name ?? t("这个 Agent");
  const group = seat.group ? view?.groups.find((g) => g.id === seat.group) : undefined;

  const pick = async (ref: string) => {
    try {
      await port.setModel(ref);
      setPicked(ref);
      onModel();
      if (team && agent && !view?.readOnly) await team.updateAgent(agent.id, { model: ref });
    } catch (e) {
      pushToast(reason(e));
    }
  };

  return (
    <section className="team-hero" aria-label={name}>
      {agent && <AgentAvatar agent={agent} size={48} />}
      {seat.group ? (
        <p className="team-hero-say">{t("这是 {name} 在「{group}」里的对话：群里轮到它发言时，消息会出现在这里。", { name, group: group?.name || t("群聊") })}</p>
      ) : (
        <>
          <p className="team-hero-say">{agent ? greeting(agent.name, agent.role, !!agent.main) : t("你好，有什么需要我做的？")}</p>
          {models.length > 0 && (
            <div className="team-hero-models" role="group" aria-label={t("为 {name} 选一个模型", { name })}>
              {models.slice(0, MAX_CHIPS).map((m) => (
                <button key={m.ref} type="button" data-action="team.hero-model" data-value={m.ref} aria-pressed={m.ref === picked} title={m.displayName || m.provider}
                  onClick={() => void pick(m.ref)}>
                  {m.modelAlias || m.model}
                </button>
              ))}
            </div>
          )}
          {agent?.main && (
            <div className="team-hero-starters">
              {STARTERS.map((s) => (
                <button key={s} type="button" data-action="team.hero-starter" data-value={s} onClick={() => onStarter(t(s))}>
                  {t(s)}
                </button>
              ))}
            </div>
          )}
        </>
      )}
    </section>
  );
}
