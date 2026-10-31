import { t } from "../../i18n";
import { reason } from "../../i18n/kernel";
import type { HubPort } from "../../port/hub";
import { useTeam } from "../../state/team";
import { setTeamMotion, useTeamMotion, type TeamMotion } from "../../state/teamMotion";
import { pushToast } from "../../state/toasts";
import { Group } from "../Group";
import { Switch } from "../Switch";
import { ScheduleList } from "./ScheduleList";
import { SharedMemoryList } from "./SharedMemoryList";

const MOTIONS: [TeamMotion, string][] = [
  ["quiet", "安静"],
  ["normal", "正常"],
  ["lively", "活泼"],
];

// The team's page in settings: how the agents move, and what was hidden from
// the sidebar.
export function TeamSection({ hub }: { hub: HubPort }) {
  const motion = useTeamMotion();
  const { view, team, shown } = useTeam(hub);
  const hidden = [
    ...(view?.agents ?? []).filter((a) => a.hidden).map((a) => ({ kind: "agent" as const, id: a.id, name: a.name })),
    ...(view?.groups ?? []).filter((g) => g.hidden && !g.archived).map((g) => ({ kind: "group" as const, id: g.id, name: g.name || t("群聊") })),
  ];
  const show = async (kind: "agent" | "group", id: string) => {
    if (!team) return;
    try {
      shown(await (kind === "agent" ? team.updateAgent(id, { hidden: false }) : team.updateGroup(id, { hidden: false })));
    } catch (e) {
      pushToast(reason(e));
    }
  };
  return (
    <>
      <Group id="team-motion" title={t("Agent 动效")} hint={t("安静只保留表示状态的动画；正常还会回应指针，并让侧栏里换了位置的行滑过去；活泼再加上空闲时的眨眼和小动作。系统开启「减少动态效果」时全部关闭。")}>
        <div className="seg" data-text role="group" aria-label={t("Agent 动效")}>
          {MOTIONS.map(([id, label]) => (
            <button key={id} type="button" data-action="team.motion" data-value={id} aria-pressed={motion === id} onClick={() => setTeamMotion(id)}>
              {t(label)}
            </button>
          ))}
        </div>
      </Group>
      {view && team && (
        <Group id="team-schedules" title={t("定时任务")} hint={t("到了设定的时间，把任务作为调度记录交给 Agent 或群聊，不视为你刚发出的消息；敏感操作仍需审批。只在 Reasonix 开着时运行，错过的时刻不会补发。")}>
          <ScheduleList team={team} view={view} />
        </Group>
      )}
      {view && team && (
        <Group id="team-memory-review" title={t("自动整理记忆")} hint={t("你发过消息的一轮结束后，用标题模型读一遍新的对话，把值得长期记住的事整理进这个 Agent 自己的记忆（群聊里整理进群记忆）。每轮一次简短请求；只有 Agent 之间往来、没有你的话的一轮不会触发。")}>
          <div className="lrow">
            <span className="tx">
              <span className="lb">{t("自动整理记忆")}</span>
              <span className="ds">{t("默认关闭；开启后会将你的新消息和已有记忆发送给标题模型（未配置时使用默认聊天模型），不发送 Agent 回复。整理结果会显示在对应会话或群聊中。")}</span>
            </span>
            <Switch data-action="team.memory-review" on={view.prefs?.memoryReview ?? false} busy={!!view.readOnly} label={t("自动整理记忆")}
              onClick={() => void team.setPrefs({ memoryReview: !(view.prefs?.memoryReview ?? false) }).then(shown, (e) => pushToast(reason(e)))} />
          </div>
        </Group>
      )}
      {view && team && (
        <Group id="team-shared-memory" title={t("团队共享记忆")} hint={t("Agent 用 remember_shared 记下、整个团队都能读到的事。只有主 Agent 和群管理员能写；这里可以让团队忘掉其中一条。")}>
          <SharedMemoryList team={team} tier="team" revision={view.revision} readOnly={!!view.readOnly} label={t("团队共享记忆")} empty={t("团队还没有共享记忆")} />
        </Group>
      )}
      {view && (
        <Group id="team-hidden" title={t("已隐藏的 Agent 和群聊")} hint={t("隐藏只是不在侧栏里列出：它们照常工作，也照常收发消息。")}>
          {hidden.length === 0 ? (
            <p className="team-note">{t("没有隐藏的 Agent 或群聊")}</p>
          ) : (
            <ul className="team-hidden-list" aria-label={t("已隐藏")}>
              {hidden.map((h) => (
                <li key={h.id}>
                  <span>{h.name}</span>
                  <button type="button" data-action="team.hide" data-target={h.id} data-value="show" disabled={!!view.readOnly}
                    aria-label={t("在侧栏显示 {name}", { name: h.name })} onClick={() => void show(h.kind, h.id)}>
                    {t("在侧栏显示")}
                  </button>
                </li>
              ))}
            </ul>
          )}
        </Group>
      )}
    </>
  );
}
