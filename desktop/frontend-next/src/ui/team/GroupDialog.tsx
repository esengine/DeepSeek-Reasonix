import { useState } from "react";
import { t } from "../../i18n";
import { reason } from "../../i18n/kernel";
import type { GroupEdit, TeamAgent, TeamGroup, TeamPort, TeamView } from "../../port/team";
import { AgentAvatar } from "./AgentAvatar";
import { TeamDialog } from "./TeamDialog";

// Creating a group chat, or changing one. The relay limit is the person's
// brake on agents calling each other, so it lives here and nowhere an agent
// reaches.
export function GroupDialog({ team, agents, group, onClose, onSaved }: {
  team: TeamPort;
  agents: TeamAgent[];
  group?: TeamGroup;
  onClose: () => void;
  onSaved: (view: TeamView) => void;
}) {
  const [name, setName] = useState(group?.name ?? "");
  const [notice, setNotice] = useState(group?.notice ?? "");
  const [members, setMembers] = useState<string[]>(group?.members ?? []);
  const [admin, setAdmin] = useState(group?.admin ?? "");
  const [mode, setMode] = useState<"admin" | "everyone">(group?.mode ?? "admin");
  const [limit, setLimit] = useState(group?.relayLimit ? String(group.relayLimit) : "");
  const [busy, setBusy] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [error, setError] = useState("");

  const toggle = (id: string) => {
    setMembers((cur) => (cur.includes(id) ? cur.filter((m) => m !== id) : [...cur, id]));
    if (admin === id) setAdmin("");
  };
  const run = async (write: () => Promise<TeamView>) => {
    setBusy(true);
    setError("");
    try {
      onSaved(await write());
    } catch (e) {
      setError(reason(e));
      setBusy(false);
    }
  };
  const save = () => {
    const edit: GroupEdit = { name: name.trim(), notice, members, admin, mode: admin ? mode : "everyone" };
    if (limit.trim()) edit.relayLimit = Number(limit);
    return run(() => (group ? team.updateGroup(group.id, edit) : team.createGroup(edit)));
  };

  return (
    <TeamDialog title={group ? t("群聊设置") : t("新建群聊")} busy={busy} onClose={onClose} onSubmit={() => void save()} target={group?.id}
      footer={<>
        {group && (
          <button type="button" className="team-danger" data-action={confirmDelete ? "team.group-delete" : "team.group-delete-ask"} data-target={group.id} disabled={busy}
            onClick={() => (confirmDelete ? void run(() => team.deleteGroup(group.id)) : setConfirmDelete(true))}>
            {confirmDelete ? t("确认删除") : t("删除")}
          </button>
        )}
        <button type="button" data-action="layer.dismiss" disabled={busy} onClick={onClose}>{t("取消")}</button>
        <button type="submit" className="team-save" disabled={busy || members.length < 2}>
          {group ? t("保存") : t("创建")}
        </button>
      </>}>
      <label>{t("名字")}
        <input data-action="team.group-name" value={name} maxLength={60} placeholder={t("留空则按成员命名")} autoFocus onChange={(ev) => setName(ev.target.value)} />
      </label>
      <label>{t("公告")}
        <textarea data-action="team.group-notice" value={notice} placeholder={t("这个群在做什么，每位成员都能看到")} onChange={(ev) => setNotice(ev.target.value)} />
      </label>
      <fieldset className="team-members">
        <legend>{t("成员")}</legend>
        {agents.map((a) => (
          <label key={a.id} className="team-check">
            <input type="checkbox" data-action="team.group-member" data-target={a.id} checked={members.includes(a.id)} onChange={() => toggle(a.id)} />
            <AgentAvatar agent={a} size={20} badges={false} />
            {a.name}
          </label>
        ))}
      </fieldset>
      <label>{t("管理员")}
        <select data-action-change="team.group-admin" value={admin} onChange={(ev) => setAdmin(ev.target.value)}>
          <option value="">{t("无")}</option>
          {agents.filter((a) => members.includes(a.id)).map((a) => <option key={a.id} value={a.id}>{a.name}</option>)}
        </select>
      </label>
      <label>{t("谁来回答你的消息")}
        <select data-action-change="team.group-mode" value={admin ? mode : "everyone"} disabled={!admin} onChange={(ev) => setMode(ev.target.value as "admin" | "everyone")}>
          <option value="admin">{t("管理员回答")}</option>
          <option value="everyone">{t("每位成员依次回答")}</option>
        </select>
      </label>
      <label>{t("Agent 之间接力几次后暂停")}
        <input data-action="team.group-limit" type="number" min={1} max={200} value={limit} placeholder={String((members.length || 2) * 3)} onChange={(ev) => setLimit(ev.target.value)} />
      </label>
      {error && <p className="team-error" role="alert">{error}</p>}
    </TeamDialog>
  );
}
