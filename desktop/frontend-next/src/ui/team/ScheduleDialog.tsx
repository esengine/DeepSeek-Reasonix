import { useState } from "react";
import { t } from "../../i18n";
import { reason } from "../../i18n/kernel";
import type { TeamPort, TeamSchedule, TeamView } from "../../port/team";
import { TeamDialog } from "./TeamDialog";

const REPEATS: [TeamSchedule["repeat"], string][] = [
  ["once", "一次"],
  ["daily", "每天"],
  ["weekly", "每周"],
  ["every", "每隔一段时间"],
];
const WEEKDAYS = ["日", "一", "二", "三", "四", "五", "六"];

// A local "YYYY-MM-DDTHH:MM" for a datetime input, an hour from now.
function inAnHour(): string {
  const d = new Date(Date.now() + 3600_000);
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`;
}

// Setting a task for an agent or a group, or changing one. The kernel checks
// what it keeps; a refusal names the field it is about.
export function ScheduleDialog({ team, view, task, onClose, onSaved }: {
  team: TeamPort;
  view: TeamView;
  task?: TeamSchedule;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [target, setTarget] = useState(task?.agent ? `agent:${task.agent}` : task?.group ? `group:${task.group}` : `agent:${view.agents[0]?.id ?? ""}`);
  const [name, setName] = useState(task?.name ?? "");
  const [prompt, setPrompt] = useState(task?.prompt ?? "");
  const [repeat, setRepeat] = useState<TeamSchedule["repeat"]>(task?.repeat ?? "daily");
  const [clock, setClock] = useState(task?.clock ?? "09:00");
  const [days, setDays] = useState<number[]>(task?.days ?? [1]);
  const [every, setEvery] = useState(String(task?.every ?? 1));
  const [unit, setUnit] = useState<NonNullable<TeamSchedule["unit"]>>(task?.unit ?? "hours");
  const [once, setOnce] = useState(task?.once ? task.once.slice(0, 16) : inAnHour());
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const save = async () => {
    const [kind, id] = target.split(":");
    const body: TeamSchedule = { id: task?.id, name: name.trim(), prompt, repeat, enabled: task?.enabled ?? true, ...(kind === "agent" ? { agent: id } : { group: id }) };
    if (repeat === "daily" || repeat === "weekly") body.clock = clock;
    if (repeat === "weekly") body.days = days;
    if (repeat === "every") Object.assign(body, { every: Number(every), unit, once: new Date().toISOString() });
    if (repeat === "once") body.once = new Date(once).toISOString();
    setBusy(true);
    setError("");
    try {
      await team.saveSchedule(body);
      onSaved();
    } catch (e) {
      setError(reason(e));
      setBusy(false);
    }
  };

  return (
    <TeamDialog title={task ? t("编辑定时任务") : t("新建定时任务")} busy={busy} onClose={onClose} onSubmit={() => void save()} target={task?.id}
      footer={<>
        <button type="button" data-action="layer.dismiss" disabled={busy} onClick={onClose}>{t("取消")}</button>
        <button type="submit" className="team-save" disabled={busy || !prompt.trim() || !target.split(":")[1]}>{t("保存")}</button>
      </>}>
      <label>{t("发给")}
        <select data-action-change="team.schedule-target" value={target} onChange={(ev) => setTarget(ev.target.value)}>
          {view.agents.map((a) => <option key={a.id} value={`agent:${a.id}`}>{a.name}</option>)}
          {view.groups.filter((g) => !g.archived).map((g) => <option key={g.id} value={`group:${g.id}`}>{t("群聊：{name}", { name: g.name || g.id })}</option>)}
        </select>
      </label>
      <label>{t("名称")}
        <input data-action="team.schedule-name" value={name} maxLength={60} onChange={(ev) => setName(ev.target.value)} />
      </label>
      <label>{t("要做的事")}
        <textarea data-action="team.schedule-prompt" value={prompt} maxLength={2000} placeholder={t("到时间时发给它的话")} onChange={(ev) => setPrompt(ev.target.value)} />
      </label>
      <label>{t("重复方式")}
        <select data-action-change="team.schedule-repeat" value={repeat} onChange={(ev) => setRepeat(ev.target.value as TeamSchedule["repeat"])}>
          {REPEATS.map(([v, l]) => <option key={v} value={v}>{t(l)}</option>)}
        </select>
      </label>
      {repeat === "once" && (
        <label>{t("时刻")}
          <input type="datetime-local" data-action="team.schedule-once" value={once} onChange={(ev) => setOnce(ev.target.value)} />
        </label>
      )}
      {(repeat === "daily" || repeat === "weekly") && (
        <label>{t("时间")}
          <input type="time" data-action="team.schedule-clock" value={clock} onChange={(ev) => setClock(ev.target.value)} />
        </label>
      )}
      {repeat === "weekly" && (
        <fieldset className="team-weekdays">
          <legend>{t("星期")}</legend>
          {WEEKDAYS.map((d, i) => (
            <label key={d} className="team-check">
              <input type="checkbox" data-action="team.schedule-day" data-value={String(i)} checked={days.includes(i)}
                onChange={() => setDays((cur) => (cur.includes(i) ? cur.filter((x) => x !== i) : [...cur, i].sort()))} />
              {t("周{day}", { day: t(d) })}
            </label>
          ))}
        </fieldset>
      )}
      {repeat === "every" && (
        <div className="team-every">
          <label>{t("间隔")}
            <input type="number" min={1} data-action="team.schedule-every" value={every} onChange={(ev) => setEvery(ev.target.value)} />
          </label>
          <label>{t("单位")}
            <select data-action-change="team.schedule-unit" value={unit} onChange={(ev) => setUnit(ev.target.value as NonNullable<TeamSchedule["unit"]>)}>
              <option value="minutes">{t("分钟")}</option>
              <option value="hours">{t("小时")}</option>
              <option value="days">{t("天")}</option>
            </select>
          </label>
          <p className="team-note">{t("最短 10 分钟一次")}</p>
        </div>
      )}
      {error && <p className="team-error" role="alert">{error}</p>}
    </TeamDialog>
  );
}
