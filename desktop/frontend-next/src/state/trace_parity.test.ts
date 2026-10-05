import { describe, expect, it } from "vitest";
import readings from "../e2e/fixtures/trace_readings.json";
import { initialState, reduce } from "./session";
import { initialTraj, reduceTraj } from "./trajectory";
import type { SessionState } from "./session_types";
import type { WireEvent } from "../port/wire";

type Reading = { name: string; live: WireEvent[]; replay: WireEvent[] };
const fixtures = readings as unknown as Record<string, Reading>;

// What survives a reload is what this compares. Client-minted ids are not facts
// about the turn — two readings that agree on everything else still mint
// different ones — so the projection drops them and keeps what the kernel said.
function durableItems(s: SessionState) {
  return s.items
    .filter((i) => i.t !== "user")
    .map((i) => {
      switch (i.t) {
        case "say":
          return { t: i.t, source: i.source ?? null, hasText: i.text.length > 0, done: i.done };
        case "tool":
          return {
            t: i.t,
            name: i.tool.name,
            toolId: i.tool.id ?? null,
            issuer: i.tool.issuer ?? null,
            running: i.running,
            children: i.children.length,
          };
        default:
          return { t: i.t };
      }
    });
}

// The trajectory table is the other durable projection off the same frames, and
// it folds different things: rows, not cards.
function trajRows(frames: WireEvent[]) {
  let s = initialTraj;
  for (const ev of frames) s = reduceTraj(s, ev);
  return s.rows.map((r) => ({ kind: r.kind, tool: r.tool ?? null }));
}

// A fresh store every time. Reusing one and clearing it is what lets a reload
// borrow state the live run left behind — turn-local scratch, a streaming say,
// a partial tool card — and pass while a real reload would not.
function foldFresh(frames: WireEvent[]) {
  let s = initialState;
  for (const ev of frames) s = reduce(s, ev);
  return s;
}

describe("a reload rebuilds the same conversation the live stream drew", () => {
  for (const [name, reading] of Object.entries(fixtures)) {
    it(name, () => {
      const live = foldFresh(reading.live);
      // Nothing above this line is reachable below it: the cold reading starts
      // from the module's own initial state, not from `live`.
      const cold = foldFresh(reading.replay);
      // An empty projection matches an empty projection. Say what the fixture is
      // supposed to contain before comparing, or the gate passes on nothing.
      expect(durableItems(live).length).toBeGreaterThan(0);
      expect(trajRows(reading.live).length).toBeGreaterThan(0);
      expect(durableItems(cold)).toEqual(durableItems(live));
      expect(trajRows(reading.replay)).toEqual(trajRows(reading.live));
    });
  }

  // The destructive control: the cold reading loses the producer, and the
  // comparison has to notice. A parity test that passes without the field is a
  // test that was never reading it.
  it("notices when the cold reading loses a producer", () => {
    const dual = fixtures["dual_model"];
    expect(dual).toBeDefined();
    const blind = dual.replay.map((e) => ({ ...e, source: undefined }));
    expect(durableItems(foldFresh(blind))).not.toEqual(durableItems(foldFresh(dual.live)));
  });

  // And when it loses who issued a call.
  it("notices when the cold reading loses an issuer", () => {
    const single = fixtures["single_model"];
    expect(single).toBeDefined();
    const blind = single.replay.map((e) =>
      e.tool ? { ...e, tool: { ...e.tool, issuer: undefined } } : e,
    );
    expect(durableItems(foldFresh(blind))).not.toEqual(durableItems(foldFresh(single.live)));
  });

  it("has fixtures to run at all", () => {
    expect(Object.keys(fixtures).length).toBeGreaterThanOrEqual(4);
  });
});
