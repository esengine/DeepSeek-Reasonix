import { describe, expect, it } from "vitest";
import { chipLabel, initialState, reduce, type SessionEvent, type SessionState } from "./session";

const request: SessionEvent = {
  kind: "ask_request",
  ask: { id: "greeting", questions: [{ id: "name", header: "Your name", prompt: "Your name", multi: false, options: [] }] },
};
const receipt: SessionEvent = {
  kind: "notice",
  decisionReceipt: { id: "greeting", kind: "ask", subject: "Your name: Native SDK", outcome: "answered" },
};
const settled = () => reduce(reduce(initialState, { kind: "turn_started" }), { kind: "turn_done" });

function answer(s: SessionState, route: "local" | "receipt") {
  const item = s.items.find((i) => i.t === "ask")!;
  return reduce(s, route === "receipt" ? receipt : { kind: "__decided", id: item.id, answers: [["Native SDK"]] });
}

for (const route of ["local", "receipt"] as const) {
  describe(`${route} question answer`, () => {
    it.each(["fresh", "settled"] as const)("returns an idle %s session to idle without inventing a running turn", (prior) => {
      const waiting = reduce(prior === "fresh" ? initialState : settled(), request);
      expect(chipLabel(waiting, false)).toBe("等待确认");
      const done = answer(waiting, route);
      expect(done.running).toBe(false);
      expect(chipLabel(done, false)).toBe("空闲");
      expect(done.items.find((i) => i.t === "ask")).toMatchObject({ answered: route === "local" ? [["Native SDK"]] : [] });
    });

    it("resumes the running label when the question belongs to a live turn", () => {
      const waiting = reduce(reduce(initialState, { kind: "turn_started" }), request);
      const resumed = answer(waiting, route);
      expect(resumed.running).toBe(true);
      expect(chipLabel(resumed, true)).toBe("运行中");
    });

    it("lets the current status identify a paired turn that missed turn_started", () => {
      const resumed = answer(reduce(settled(), request), route);
      expect(resumed.running).toBe(false);
      expect(chipLabel(resumed, true)).toBe("运行中");
    });
  });
}

it("keeps a local answer when its receipt arrives afterward", () => {
  const answered = answer(reduce(settled(), request), "local");
  const replayed = reduce(answered, receipt);
  expect(chipLabel(replayed, false)).toBe("空闲");
  expect(replayed.items.find((i) => i.t === "ask")).toMatchObject({ answered: [["Native SDK"]], answeredElsewhere: false });
});

it("ignores a receipt that belongs to another question", () => {
  const waiting = reduce(settled(), request);
  const unrelated = reduce(waiting, { kind: "notice", decisionReceipt: { id: "other", kind: "ask", subject: "other", outcome: "answered" } });
  expect(chipLabel(unrelated, false)).toBe("等待确认");
  expect(unrelated.items.find((i) => i.t === "ask")).not.toHaveProperty("answered");
});
