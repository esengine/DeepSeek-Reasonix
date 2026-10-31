import { t } from "../../i18n";
import type { HubPort } from "../../port/hub";
import type { TeamAgent } from "../../port/team";
import { useTeam } from "../../state/team";
import { toggleSeatDrawer, useSeatDrawer } from "../../state/teamDrawer";
import { AgentAvatar, GroupAvatar } from "./AgentAvatar";
import "../../styles/team.css";

// The chrome's crumb for the team: the agent whose seat the pane holds and its
// group when it has one, or the group whose chat covers the panes. The roster
// owns every name.
export function SeatCrumb({ hub, agent: agentId, group: groupId }: { hub: HubPort; agent?: string; group?: string }) {
  const { view } = useTeam(hub);
  const open = useSeatDrawer();
  const agent = agentId ? view?.agents.find((a) => a.id === agentId) : undefined;
  const group = groupId ? view?.groups.find((g) => g.id === groupId) : undefined;
  if (!agentId) {
    const members = (group?.members ?? []).map((id) => view?.agents.find((a) => a.id === id)).filter((a): a is TeamAgent => !!a);
    return (
      <span className="crumb-seat">
        <GroupAvatar members={members} size={18} />
        <span className="crumb-proj">{group?.name || members.map((m) => m.name).join("、") || t("群聊")}</span>
      </span>
    );
  }
  return (
    <span className="crumb-seat">
      <button type="button" className="crumb-seat-toggle" data-action="team.details" aria-expanded={open} title={t("详情")} onClick={toggleSeatDrawer}>
        {agent && <AgentAvatar agent={agent} size={18} badges={false} />}
        <span className="crumb-proj">{agent?.name ?? t("Agent")}</span>
      </button>
      {groupId && <span className="isolab">{group?.name || t("群聊")}</span>}
    </span>
  );
}
