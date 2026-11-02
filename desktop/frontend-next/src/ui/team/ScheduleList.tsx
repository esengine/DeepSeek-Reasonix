import { useEffect, useState } from "react";
import { t } from "../../i18n";
import { reason } from "../../i18n/kernel";
import type { TeamPort, TeamSchedule, TeamView } from "../../port/team";
import { pushToast } from "../../state/toasts";
import { Switch } from "../Switch";
import { ScheduleDialog } from "./ScheduleDialog";
import { describeSchedule } from "./schedules";

// The team's scheduled tasks: who each goes to and when, a switch to pause it,
// and removal that takes a second press.
export function ScheduleList({ team, view }: { team: TeamPort; view: TeamView }) {
  const [tasks, setTasks] = useState<TeamSchedule[] | null>(null);
  const [editing, setEditing] = useState<TeamSchedule | "new" | null>(null);
  const [confirming, setConfirming] = useState("");
  const load = () => team.schedules().then(setTasks, (e) => pushToast(reason(e)));
  useEffect(() => void load(), [team, view.revision]);
  const names = new Map<string, string>([...view.agents.map((a) => [a.id, a.name] as const), ...view.groups.map((g) => [g.id, g.name || t("群聊")] as const)]);
  const readOnly = !!view.readOnly;
  const write = async (run: () => Promise<unknown>) => {
    try {
      await run();
      await load();
    } catch (e) {
      pushToast(reason(e));
    }
  };
  const remove = (s: TeamSchedule) => {
    if (confirming !== s.id) {
      setConfirming(s.id ?? "");
      return;
    }
    setConfirming("");
    void write(() => team.deleteSchedule(s.id!));
  };

  return (
    <>
      {tasks && tasks.length === 0 && <p className="team-note">{t("还没有定时任务")}</p>}
      {tasks && tasks.length > 0 && (
        <ul className="team-shared team-schedules" aria-label={t("定时任务")}>
          {tasks.map((s) => (
            <li key={s.id}>
              <span className="team-shared-text">
                <b>{s.name || t("定时任务")}</b>
                <span>{`${names.get(s.agent ?? s.group ?? "") ?? t("已不在团队里")} · ${describeSchedule(s)}`}</span>
              </span>
              {!readOnly && (
                <>
                  <button type="button" data-action="team.schedule-edit" data-target={s.id} onClick={() => setEditing(s)}>{t("编辑")}</button>
                  <Switch data-action="team.schedule-enable" on={s.enabled} label={t("启用 {name}", { name: s.name })}
                    onClick={() => void write(() => team.saveSchedule({ ...s, enabled: !s.enabled }))} />
                  <button type="button" data-action={confirming === s.id ? "team.schedule-delete" : "team.schedule-delete-ask"} data-target={s.id}
                    aria-label={confirming === s.id ? t("确认删除 {name}", { name: s.name }) : t("删除 {name}", { name: s.name })} onClick={() => remove(s)}>
                    {confirming === s.id ? t("确认删除") : t("删除")}
                  </button>
                </>
              )}
            </li>
          ))}
        </ul>
      )}
      {!readOnly && view.agents.length > 0 && (
        <button type="button" className="team-drawer-link" data-action="team.schedule-new" onClick={() => setEditing("new")}>{t("新建定时任务")}</button>
      )}
      {editing && (
        <ScheduleDialog team={team} view={view} task={editing === "new" ? undefined : editing} onClose={() => setEditing(null)}
          onSaved={() => { setEditing(null); void load(); }} />
      )}
    </>
  );
}
