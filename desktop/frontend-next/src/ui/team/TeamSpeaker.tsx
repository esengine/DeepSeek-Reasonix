import { createContext, useContext, type ReactNode } from "react";
import { t } from "../../i18n";
import type { RuntimeView } from "../../port/hub";
import type { TeamAgent } from "../../port/team";
import { useCurrentTeam } from "../../state/team";

type Speaker = Pick<TeamAgent, "id" | "name" | "avatar" | "main">;
const ReplySpeaker = createContext<Speaker | null>(null);

// A reply belongs to its runtime's seat, including a member's group seat.
// The selected sidebar row and a delivered message's sender cannot identify it.
export function TeamSpeaker({ seat, children }: { seat?: RuntimeView["team"]; children: ReactNode }) {
  const { view } = useCurrentTeam();
  const agent = seat ? view?.agents.find((a) => a.id === seat.agent) ?? { id: seat.agent, name: t("另一个 Agent"), avatar: {} } : null;
  return <ReplySpeaker.Provider value={agent}>{children}</ReplySpeaker.Provider>;
}

export function useReplySpeaker() {
  return useContext(ReplySpeaker);
}
