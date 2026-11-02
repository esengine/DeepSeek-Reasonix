import { t } from "../../i18n";
import { reason } from "../../i18n/kernel";
import type { HubPort } from "../../port/hub";
import { useTeam } from "../../state/team";
import { setTeamMotion, useTeamMotion, type TeamMotion } from "../../state/teamMotion";
import { pushToast } from "../../state/toasts";
import { Group } from "../Group";

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
