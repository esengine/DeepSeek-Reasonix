import type { Checkpoint } from "../port/port";
import type { Item } from "../state/session";
import { pairCheckpoints } from "../state/checkpoints";
import { opensTurn } from "./blocks";

export function forkTargets(items: Item[], checkpoints: Checkpoint[]): Map<string, Checkpoint> {
  const paired = pairCheckpoints(items, checkpoints);
  const targets = new Map<string, Checkpoint>();
  let checkpoint: Checkpoint | undefined;
  let final: Extract<Item, { t: "say" }> | undefined;
  const finish = () => {
    if (checkpoint?.canFork && checkpoint.stamp && checkpoint.msgIndex !== undefined && final?.done && final.text.trim()) {
      targets.set(final.id, checkpoint);
    }
  };
  for (const item of items) {
    if (opensTurn(item)) {
      finish();
      checkpoint = paired.get(item.id);
      final = undefined;
    } else if (item.t === "say") {
      final = item;
    } else if (item.t === "tool" || item.t === "reads") {
      final = undefined;
    }
  }
  finish();
  return targets;
}
