import { useEffect, useLayoutEffect, useRef, useState, type RefObject } from "react";
import { createPortal } from "react-dom";
import { t } from "../../i18n";
import { reason } from "../../i18n/kernel";
import type { RuntimeView } from "../../port/hub";
import type { TeamAgent, TeamGroup, TeamPort, TeamView } from "../../port/team";
import { markUnread, noteSeen } from "../../state/teamUnread";
import { pushToast } from "../../state/toasts";
import { useDismiss } from "../dismiss";
import { pinToViewport } from "../place";
import { StudioIcon } from "../StudioIcon";

export type MenuTarget = { kind: "agent"; agent: TeamAgent } | { kind: "group"; group: TeamGroup; title: string };

// The first free "X 副本" name, so a second copy does not trip over the first.
function copyName(name: string, taken: TeamAgent[]): string {
  const used = new Set(taken.map((a) => a.name.toLowerCase()));
  const first = t("{name} 副本", { name });
  if (!used.has(first.toLowerCase())) return first;
  for (let n = 2; ; n++) {
    const next = `${first} ${n}`;
    if (!used.has(next.toLowerCase())) return next;
  }
}

// What can be done to one agent or group from its row, drawn at the document
// root so the rail's clipping cannot cut it. Deleting takes a second press.
export function TeamMenu({ team, view, target, at, unread, lead, anchor, onClose, onSettings, onShown, onOpened }: {
  team: TeamPort;
  view: TeamView;
  target: MenuTarget;
  at: { x: number; y: number };
  unread: boolean;
  lead: boolean;
  anchor: RefObject<HTMLElement | null>;
  onClose: () => void;
  onSettings: () => void;
  onShown: (view: TeamView) => void;
  onOpened: (rt: RuntimeView | Promise<RuntimeView>) => void;
}) {
  const menu = useRef<HTMLDivElement>(null);
  const [confirming, setConfirming] = useState(false);
  useDismiss(true, anchor, onClose, menu);
  useLayoutEffect(() => {
    if (menu.current) pinToViewport(menu.current, at.x, at.y, 12);
  }, [at]);
  useEffect(() => {
    menu.current?.querySelector<HTMLButtonElement>("button:not(:disabled)")?.focus();
  }, []);

  const id = target.kind === "agent" ? target.agent.id : target.group.id;
  const name = target.kind === "agent" ? target.agent.name : target.title;
  const pinned = target.kind === "agent" ? !!target.agent.pinned : !!target.group.pinned;
  const hidden = target.kind === "agent" ? !!target.agent.hidden : !!target.group.hidden;
  const last = target.kind === "agent" ? target.agent.lastActivityAt : target.group.lastActivityAt;
  const mains = view.agents.filter((a) => a.main).length;

  const write = async (run: () => Promise<TeamView>) => {
    onClose();
    try {
      onShown(await run());
    } catch (e) {
      pushToast(reason(e));
    }
  };
  const change = (edit: { pinned?: boolean; hidden?: boolean; main?: boolean }) =>
    write(() => (target.kind === "agent" ? team.updateAgent(id, edit) : team.updateGroup(id, edit)));
  const duplicate = async () => {
    if (target.kind !== "agent") return;
    const a = target.agent;
    const copy = copyName(a.name, view.agents);
    onClose();
    try {
      const next = await team.createAgent({ name: copy, role: a.role, instructions: a.instructions, avatar: a.avatar, model: a.model, folder: a.folder, permission: a.permission });
      onShown(next);
      const made = next.agents.find((x) => x.name === copy);
      if (made) await onOpened(team.openAgent(made.id));
    } catch (e) {
      pushToast(reason(e));
    }
  };
  const remove = () => {
    if (!confirming) {
      setConfirming(true);
      return;
    }
    void write(async () => (target.kind === "agent" ? team.deleteAgent(id) : team.deleteGroup(id)));
  };

  return createPortal(
    <div ref={menu} className="session-pop team-pop" role="menu" aria-label={t("{name} 的操作", { name })}>
      <div className="session-pop-head">
        <b>{name}</b>
        <small>{target.kind === "agent" ? target.agent.role || t("Agent") : t("群聊")}</small>
      </div>
      <div className="session-pop-group">
        {!lead && (
          <button role="menuitem" data-action="team.pin" data-target={id} onClick={() => void change({ pinned: !pinned })}>
            <StudioIcon name="pin" /><span>{pinned ? t("取消置顶") : t("置顶")}</span>
          </button>
        )}
        {unread ? (
          <button role="menuitem" data-action="team.mark-read" data-target={id} onClick={() => { noteSeen([{ id, at: last, looking: true }]); onClose(); }}>
            <StudioIcon name="check" /><span>{t("标为已读")}</span>
          </button>
        ) : (
          <button role="menuitem" data-action="team.mark-unread" data-target={id} onClick={() => { markUnread(id); onClose(); }}>
            <StudioIcon name="eye" /><span>{t("标为未读")}</span>
          </button>
        )}
        {target.kind === "agent" ? (
          <button role="menuitem" data-action="team.agent-settings" data-target={id} onClick={() => { onClose(); onSettings(); }}>
            <StudioIcon name="settings" /><span>{t("设置")}</span>
          </button>
        ) : (
          <button role="menuitem" data-action="team.group-settings" data-target={id} onClick={() => { onClose(); onSettings(); }}>
            <StudioIcon name="settings" /><span>{t("设置")}</span>
          </button>
        )}
        {target.kind === "agent" && (
          <button role="menuitem" data-action="team.duplicate" data-target={id} onClick={() => void duplicate()}>
            <StudioIcon name="copy" /><span>{t("复制 Agent")}</span>
          </button>
        )}
        {target.kind === "agent" && !target.agent.main && (
          <button role="menuitem" data-action="team.set-main" data-target={id} data-value="on" onClick={() => void change({ main: true })}>
            <StudioIcon name="spark" /><span>{t("设为主 Agent")}</span>
          </button>
        )}
        {target.kind === "agent" && target.agent.main && mains > 1 && (
          <button role="menuitem" data-action="team.set-main" data-target={id} data-value="off" onClick={() => void change({ main: false })}>
            <StudioIcon name="spark" /><span>{t("取消主 Agent")}</span>
          </button>
        )}
        {!lead && (
          <button role="menuitem" data-action="team.hide" data-target={id} data-value={hidden ? "show" : "hide"} onClick={() => void change({ hidden: !hidden })}>
            <StudioIcon name={hidden ? "eye" : "eyeoff"} /><span>{hidden ? t("在侧栏显示") : t("从侧栏隐藏")}</span>
          </button>
        )}
      </div>
      {!(target.kind === "agent" && target.agent.main) && (
        <>
          <div className="session-pop-divider" />
          <div className="session-pop-group">
            <button className="danger" role="menuitem" data-action={confirming ? "team.delete" : "team.delete-ask"} data-target={id} onClick={remove}>
              <StudioIcon name="trash" />
              <span>{confirming ? t("确认删除 {name}", { name }) : target.kind === "agent" ? t("删除 Agent") : t("删除群聊")}</span>
            </button>
          </div>
        </>
      )}
    </div>,
    document.body,
  );
}
