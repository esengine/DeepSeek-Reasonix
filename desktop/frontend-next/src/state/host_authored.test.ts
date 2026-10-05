import { describe, expect, it } from "vitest";
import { fromHistory } from "./session";
import type { HistoryMessage } from "../port/port";

const users = (msgs: HistoryMessage[]) => fromHistory(msgs).items.filter((i) => i.t === "user");

// The kernel says who wrote a line. Nothing here reads the wording to decide,
// which is the whole point: the two cases below differ only in the flag.
describe("a line the host composed is not a thing the user said", () => {
  const approved = "Plan approved — plan mode is off. Implement it now.";

  it("draws no bubble for a host-composed continuation", () => {
    const items = users([
      { role: "user", content: "规划一下这件事", msgIndex: 1 },
      { role: "assistant", content: "here is the plan", msgIndex: 2 },
      { role: "user", content: approved, msgIndex: 3, hostAuthored: true },
      { role: "assistant", content: "executed", msgIndex: 4 },
    ]);
    expect(items).toMatchObject([{ text: "规划一下这件事", msgIndex: 1 }]);
  });

  // The other direction, and the one a wording rule gets wrong: the user typed
  // that sentence themselves.
  it("keeps a user line that reads exactly like one", () => {
    const items = users([{ role: "user", content: approved, msgIndex: 1 }]);
    expect(items).toMatchObject([{ text: approved, msgIndex: 1 }]);
  });

  // And a host line with nothing template-shaped about it is still the host's.
  it("draws no bubble for host text that reads like anything else", () => {
    const items = users([{ role: "user", content: "完全不像任何模板的一句话", msgIndex: 1, hostAuthored: true }]);
    expect(items).toEqual([]);
  });

  it("hides the legacy unavailable-tool repair prompt from older sessions", () => {
    const items = users([{
      role: "user",
      content: "The following tools are unavailable in the current workflow phase: update_goal. Do not call them again. Respond to the user's request with visible answer text now; call a different tool only if it is still needed to complete the request.",
      msgIndex: 1,
    }]);
    expect(items).toEqual([]);
  });
});
