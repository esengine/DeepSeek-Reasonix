import { describe, expect, it } from "vitest";
import type { Tool } from "../port/wire";
import { agentsOf, isDelegation, nestLabel } from "./delegation";

const tool = (over: Partial<Tool> = {}): Tool => ({ id: "t1", name: "use_capability", ...over }) as Tool;

describe("delegation", () => {
  // Every dispatcher is reached through the proxy, so the card's name is
  // "use_capability" no matter what ran. Matching on the name reported no
  // sub-agents at all while a fleet of them was running.
  it("marks a proxied dispatch, not a tool name", () => {
    expect(isDelegation(tool({ resolvedName: "task", profile: { name: "task", count: 1 } }))).toBe(true);
    expect(isDelegation(tool({ name: "task" }))).toBe(false);
    expect(isDelegation(tool({ name: "bash" }))).toBe(false);
  });

  it("counts contexts opened, defaulting a single dispatch to one", () => {
    expect(agentsOf(tool({ profile: { name: "fleet", count: 12 } }))).toBe(12);
    expect(agentsOf(tool({ profile: { name: "task" } }))).toBe(1);
    expect(agentsOf(tool({ profile: { name: "fleet", count: 0 } }))).toBe(1);
  });

  // The regression this file exists for: a delegate's own calls are steps. The
  // label read them as a count of delegates, so one sub-agent doing 64 things
  // announced itself as 64 sub-agents.
  it("reads nested rows as the delegate's steps", () => {
    expect(nestLabel(undefined, 1, 64)).toContain("64");
    expect(nestLabel(undefined, 1, 64)).not.toContain("64 个子代理");
    expect(nestLabel("security-review", 1, 64)).toContain("security-review");
  });

  it("says both numbers when a batch opened many contexts", () => {
    const label = nestLabel(undefined, 12, 64);
    expect(label).toContain("12");
    expect(label).toContain("64");
  });
});
