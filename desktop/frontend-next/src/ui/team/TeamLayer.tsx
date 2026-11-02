import type { HubPort, RuntimeView } from "../../port/hub";
import { GroupStage } from "./GroupStage";
import { SeatDrawer } from "./SeatDrawer";

// What the team draws over the panes: a group's chat filling the area, or the
// focused agent's details beside its conversation.
export function TeamLayer({ hub, active, seat, root, onAdoptPane }: {
  hub: HubPort;
  active: string;
  seat?: RuntimeView["team"];
  root?: string;
  onAdoptPane: (rt: RuntimeView | Promise<RuntimeView>) => void;
}) {
  return (
    <>
      <GroupStage hub={hub} active={active} onAdoptPane={onAdoptPane} />
      <SeatDrawer hub={hub} seat={seat} root={root} onAdoptPane={onAdoptPane} />
    </>
  );
}
