import { describe, expect, it } from "vitest";
import { deviates, policySummary, type PolicySummary } from "./policyview";
import type { ApprovalMode, Preset, SessionStatus } from "../port/port";

const status = (preset: Preset, effort: string | undefined, mode: ApprovalMode): SessionStatus =>
  ({ preset, effort, toolApprovalMode: mode } as SessionStatus);

// What the shelf actually reads, assembled the way Policy.tsx assembles it.
const shelf = (s: PolicySummary) =>
  s.ready ? [s.preset, s.effort, s.approval].filter(Boolean).join(" · ") : "";

const at = (preset: Preset, effort: string | undefined, mode: ApprovalMode, hasEffort = true) =>
  policySummary(status(preset, effort, mode), hasEffort);

describe("the posture a session runs as by default", () => {
  // The defect: three kernel facts recited forever, on every turn where nothing
  // was unusual, next to a model picker and everything else the composer holds.
  it("spends nothing on a session nobody has configured", () => {
    expect(shelf(at("balanced", "auto", "ask"))).toBe("");
    expect(deviates(at("balanced", "auto", "ask"))).toBe(false);
  });

  // Sabotage A guards this from the other side: putting the baseline values
  // back must not read as a summary anyone would accept.
  it("never recites a value the session already had", () => {
    const s = at("balanced", "auto", "ask");
    expect(s.ready && s.preset).toBeUndefined();
    expect(s.ready && s.effort).toBeUndefined();
    expect(s.ready && s.approval).toBeUndefined();
  });

  // A missing effort is the kernel not naming one, which is what auto means.
  it("treats an absent rung as the baseline rather than as unknown", () => {
    expect(shelf(at("balanced", undefined, "ask"))).toBe("");
  });

  it("names a preset that is not the one a session starts on", () => {
    expect(shelf(at("delivery", "auto", "ask"))).toBe("交付");
    expect(deviates(at("delivery", "auto", "ask"))).toBe(true);
  });

  // A preset the kernel names and this table does not is a deviation by the
  // only test that matters: it is not what a session starts on.
  it("shows an unknown preset rather than swallowing it as baseline", () => {
    expect(shelf(at("economy" as Preset, "auto", "ask"))).toBe("economy");
  });
});

describe("what a deviation is allowed to cost the shelf", () => {
  it.each([
    ["balanced", "high", "ask", "High"],
    ["delivery", "high", "ask", "交付 · High"],
    ["balanced", "auto", "auto", "自动批准"],
    ["balanced", "auto", "dontAsk", "不询问"],
    ["balanced", "auto", "yolo", "全部放行"],
    ["delivery", "high", "yolo", "交付 · High · 全部放行"],
  ] as [Preset, string, ApprovalMode, string][])("reads %s/%s/%s as %s", (p, e, m, want) => {
    expect(shelf(at(p, e, m))).toBe(want);
  });

  // The menu can say 自动 because it sits under a 工具权限 heading; the shelf
  // cannot, with a reasoning rung standing right next to it.
  it("says what is automatic, not merely that something is", () => {
    const s = at("balanced", "auto", "auto");
    expect(s.ready && s.approval).toBe("自动批准");
    expect(s.ready && s.approval).not.toBe("自动");
  });

  // A rung table would need a row per rung an endpoint might publish.
  it("cases the kernel's own spelling instead of translating it", () => {
    const s = at("balanced", "xhigh", "ask");
    expect(s.ready && s.effort).toBe("Xhigh");
  });
});

describe("the two postures that must never be quiet", () => {
  // Sabotage B: dropping yolo from the projection. It is the state a person can
  // forget they are in, and forgetting it is what costs a workspace.
  it("keeps a fully-permitted session visible and toned", () => {
    const s = at("balanced", "auto", "yolo");
    expect(s.ready && s.approval).toBe("全部放行");
    expect(s.ready && s.danger).toBe(true);
  });

  // dontAsk is stricter than ask, not looser: it refuses what it would have
  // asked about. Hidden, it reads as "Reasonix suddenly changes nothing".
  it("keeps a session that refuses rather than asks visible, and not as danger", () => {
    const s = at("balanced", "auto", "dontAsk");
    expect(s.ready && s.approval).toBe("不询问");
    expect(s.ready && s.danger).toBeUndefined();
  });

  it("tones nothing else as danger", () => {
    for (const m of ["ask", "auto", "dontAsk"] as ApprovalMode[]) {
      const s = at("balanced", "auto", m);
      expect(s.ready && s.danger).toBeUndefined();
    }
  });
});

describe("what the shelf may claim before the kernel has answered", () => {
  // The context gauge's defect, in the one place where the fact is what the
  // agent may do to a workspace unasked.
  // Before the kernel has answered there is no deviation to report and no
  // posture to name, so the shelf says nothing rather than guessing at one.
  it("says nothing at all before it has been told a posture", () => {
    const s = policySummary(null, true);
    expect(s.ready).toBe(false);
    expect(deviates(s)).toBe(false);
    expect(shelf(s)).toBe("");
  });

  it("carries no posture at all to be read by mistake", () => {
    const s = policySummary(null, true);
    expect("preset" in s).toBe(false);
    expect("danger" in s).toBe(false);
  });
});

describe("a rung the endpoint does not publish", () => {
  // The open menu draws no ladder when the model names no levels; a summary
  // claiming one would advertise a control that is not there.
  it("is not named on the shelf even when a stale status carries one", () => {
    expect(shelf(at("balanced", "high", "ask", false))).toBe("");
    expect(deviates(at("balanced", "high", "ask", false))).toBe(false);
  });

  it("is left out of the hover reading too", () => {
    const s = at("balanced", "high", "ask", false);
    expect(s.ready && s.reading).toBe("均衡 · 询问");
  });
});

describe("what the projection hides is one pointer away", () => {
  // Nothing is deleted. The baseline stops occupying the shelf and stays on the
  // control, which is the difference between quiet and lossy.
  it("keeps the whole reading for hover", () => {
    const s = at("balanced", "auto", "ask");
    expect(s.ready && s.reading).toBe("均衡 · auto · 询问");
  });
});

// The collapse this change is: a shelf that says nothing when nothing is
// unusual. The one thing it may never collapse is the state a person can forget
// they are in.
describe("what the quiet may never swallow", () => {
  it("has something to say for every posture that is not the baseline", () => {
    const risky: [Preset, string, ApprovalMode][] = [
      ["delivery", "auto", "ask"],
      ["balanced", "high", "ask"],
      ["balanced", "auto", "auto"],
      ["balanced", "auto", "dontAsk"],
      ["balanced", "auto", "yolo"],
    ];
    for (const [p, e, m] of risky) {
      expect(deviates(at(p, e, m)), `${p}/${e}/${m} must not be quiet`).toBe(true);
    }
  });

  it("never goes quiet on a fully-permitted session, whatever else is baseline", () => {
    const s = at("balanced", "auto", "yolo");
    expect(deviates(s)).toBe(true);
    expect(shelf(s)).toContain("全部放行");
    expect(s.ready && s.danger).toBe(true);
  });
});
