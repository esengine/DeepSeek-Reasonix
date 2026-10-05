import { describe, expect, it } from "vitest";
import type { WireEvent } from "../port/wire";
import { nameTurnStart, othersSteer } from "./turn_start";
import type { SessionState } from "./session_types";

const started = (text: string, msgIndex: number, via?: { device: string; ordinal: number }) =>
  ({ kind: "turn_started", text, authoredTurn: msgIndex, msgIndex, via }) as WireEvent;

const empty = (items: SessionState["items"] = [], awaiting: string[] = []) =>
  ({ items, awaitingTurnStart: awaiting }) as unknown as SessionState;

describe("a turn another client started", () => {
  it("is drawn from the kernel's announcement, with the device it came from", () => {
    const s = nameTurnStart(empty(), started("from the phone", 3, { device: "dev-a", ordinal: 1 }));
    const row = s.items.at(-1);
    expect(row).toMatchObject({ t: "user", text: "from the phone", msgIndex: 3, via: { device: "dev-a", ordinal: 1 } });
  });

  it("is drawn once, not again over a row a rebuild already holds", () => {
    const held = [{ t: "user", id: "r", text: "from the phone", msgIndex: 3 }] as SessionState["items"];
    expect(nameTurnStart(empty(held), started("from the phone", 3)).items).toHaveLength(1);
  });

  it("stamps the device onto the row this screen sent", () => {
    const mine = [{ t: "user", id: "m", text: "hi", pending: true }] as SessionState["items"];
    const s = nameTurnStart(empty(mine, ["m"]), started("hi", 5, { device: "dev-a", ordinal: 1 }));
    expect(s.items).toHaveLength(1);
    expect(s.items[0]).toMatchObject({ id: "m", msgIndex: 5, via: { device: "dev-a", ordinal: 1 } });
  });
});

describe("guidance another client said into the running turn", () => {
  const steer = (extra: Partial<WireEvent>) => ({ kind: "steer", text: "use the other file", ...extra }) as WireEvent;

  it("is drawn, with the device it came from", () => {
    const items = othersSteer([], steer({ itemId: "i1", via: { device: "dev-a", ordinal: 1 } }));
    expect(items[0]).toMatchObject({ t: "user", text: "use the other file", steer: true, via: { device: "dev-a", ordinal: 1 } });
  });

  it("is never drawn when the host wrote it", () => {
    expect(othersSteer([], steer({ hostAuthored: true }))).toHaveLength(0);
  });

  it("is drawn once per item", () => {
    const once = othersSteer([], steer({ itemId: "i1" }));
    expect(othersSteer(once, steer({ itemId: "i1" }))).toHaveLength(1);
  });
});
