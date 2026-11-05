import { describe, expect, it } from "vitest";
import type { Item } from "../state/session";
import { compactionFold } from "./compactionfold";

const user = (id: string): Item => ({ t: "user", id, text: id }) as Item;
const say = (id: string): Item => ({ t: "say", id, text: id, done: true }) as Item;
const fold = (id: string, over: { messages?: number; done?: boolean } = {}): Item =>
  ({ t: "compaction", id, c: { messages: over.messages ?? 12 }, done: over.done ?? true }) as Item;

describe("what an earlier compaction folds away", () => {
  it("folds nothing before the second compaction: the stretch the first one closed stays", () => {
    expect(compactionFold([user("a"), say("b"), fold("c1"), user("d"), say("e")])).toEqual({ cut: 0, folded: 0 });
  });

  it("folds only what lies before the second-latest compaction, and counts its messages", () => {
    const items = [user("a"), say("b"), user("c"), say("d"), fold("c1"), user("e"), say("f"), fold("c2"), user("g")];
    expect(compactionFold(items)).toEqual({ cut: 4, folded: 4 });
  });

  it("moves one compaction forward each time, never folding the previous stretch", () => {
    const items = [user("a"), fold("c1"), user("b"), fold("c2"), user("c"), fold("c3"), user("d")];
    expect(compactionFold(items).cut).toBe(3);
    expect(compactionFold(items.slice(0, 5)).cut).toBe(1);
  });

  it("ignores a compaction that is still running or folded nothing", () => {
    const running = [user("a"), fold("c1"), user("b"), fold("c2", { done: false })];
    expect(compactionFold(running).cut).toBe(0);
    const empty = [user("a"), fold("c1"), user("b"), fold("c2", { messages: 0 }), user("c")];
    expect(compactionFold(empty).cut).toBe(0);
  });
});
