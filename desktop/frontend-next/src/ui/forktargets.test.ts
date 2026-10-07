import { expect, it } from "vitest";
import { forkTargets } from "./forktargets";
import type { Item } from "../state/session";

it("offers only the final reply of each checkpointed turn", () => {
  const items: Item[] = [
    { t: "user", id: "u1", text: "same", msgIndex: 1 },
    { t: "say", id: "middle", text: "checking", done: true },
    { t: "reads", id: "tools", tools: [] },
    { t: "user", id: "steer", text: "continue", steer: true },
    { t: "say", id: "final", text: "first answer", done: true },
    { t: "user", id: "u2", text: "same", msgIndex: 5 },
    { t: "say", id: "streaming", text: "second", done: false },
  ];
  const cp = { turn: 0, msgIndex: 1, stamp: "first", canFork: true, files: 0, prompt: "same" };
  expect([...forkTargets(items, [cp, { ...cp, turn: 1, msgIndex: 5 }]).keys()]).toEqual(["final"]);
  expect(forkTargets(items, [{ ...cp, stamp: undefined }]).size).toBe(0);
});
