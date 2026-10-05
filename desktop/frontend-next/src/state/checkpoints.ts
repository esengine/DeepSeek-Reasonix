import type { Checkpoint } from "../port/port";
import type { Item } from "./session";

// Both sides name the same message. The kernel opens a checkpoint at the index
// its turn's user message will take, and turn_started tells the client which of
// its own rows that message is — so the pairing is a join on that index. It is
// not a position: host chrome is dropped from the transcript, and a steer sits
// in it without being a turn. It is not the prompt either: two identical
// prompts are the same string and different turns.
//
// A rewind is destructive and cannot itself be undone. So a row that names no
// message gets no entry point at all: showing one against the wrong turn would
// throw away work the user never asked to lose.
export function pairCheckpoints(items: Item[], checkpoints: Checkpoint[]): Map<string, Checkpoint> {
  const byMessage = new Map<number, Checkpoint>();
  for (const cp of checkpoints) {
    if (cp.msgIndex !== undefined) byMessage.set(cp.msgIndex, cp);
  }
  const pairs = new Map<string, Checkpoint>();
  for (const item of items) {
    if (item.t !== "user" || item.msgIndex === undefined) continue;
    const cp = byMessage.get(item.msgIndex);
    if (cp) pairs.set(item.id, cp);
  }
  return pairs;
}
