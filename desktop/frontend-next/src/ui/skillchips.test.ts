import { describe, expect, it } from "vitest";
import { place, reconcile, snap, split, type SkillChip } from "./skillchips";

const probe = (at: number): SkillChip => ({ name: "probe", kind: "skill", at });

describe("skill chips", () => {
  it("moves a chip past an edit before it and keeps it through one after it", () => {
    const text = "fix /probe now";
    expect(reconcile(text, "please fix /probe now", [probe(4)], 7).chips).toEqual([probe(11)]);
    expect(reconcile(text, "fix /probe now!", [probe(4)], 15).chips).toEqual([probe(4)]);
  });

  it("types after a chip without the repeated letter being read as part of it", () => {
    const r = reconcile("/probe", "/probee", [probe(0)], 7);
    expect(r).toMatchObject({ text: "/probee", chips: [probe(0)] });
  });

  it("drops a chip whole when an edit reaches into it", () => {
    expect(reconcile("fix /probe now", "fix /prob now", [probe(4)], 9)).toEqual({ text: "fix  now", chips: [], caret: 4 });
    expect(reconcile("fix /probe now", "fix robe now", [probe(4)], 4)).toEqual({ text: "fix  now", chips: [], caret: 4 });
  });

  it("places a chip over the slash word and leaves the caret after its space", () => {
    const r = place("fix /pr now", 4, 7, [], { label: "/probe", insert: "/probe ", kind: "skill" });
    expect(r).toEqual({ text: "fix /probe now", chips: [probe(4)], caret: 11 });
    expect(place("fix /co", 4, 7, [], { label: "/compact", insert: "/compact ", kind: "builtin" })).toBeNull();
  });

  it("keeps the caret on the side of a chip it was moving toward", () => {
    expect(snap([probe(4)], 6, 10)).toBe(4);
    expect(snap([probe(4)], 6, 5)).toBe(10);
    expect(snap([probe(4)], 12, 5)).toBe(12);
  });

  it("hands the model the line with the chips taken out, in the order they sit", () => {
    const other: SkillChip = { name: "other", kind: "subagent", at: 16 };
    expect(split("tidy /probe the /other notes", [other, probe(5)])).toEqual({
      submit: "tidy the notes",
      invocations: [
        { name: "probe", kind: "skill", offset: 5 },
        { name: "other", kind: "subagent", offset: 9 },
      ],
    });
    expect(split("/probe", [probe(0)])).toEqual({ submit: "", invocations: [{ name: "probe", kind: "skill", offset: 0 }] });
  });
});
