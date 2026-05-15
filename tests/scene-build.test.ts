import { describe, expect, it } from "vitest";
import { box, frame, run, text } from "../src/cli/ui/scene/build.js";
import type { SceneFrame } from "../src/cli/ui/scene/types.js";

describe("scene builders", () => {
  it("text shorthand expands to a single run with no style", () => {
    expect(text("hello")).toEqual({
      kind: "text",
      runs: [{ text: "hello" }],
    });
  });

  it("text accepts a run array with styles", () => {
    const node = text([run("ok ", { color: "green", bold: true }), run("more")]);
    expect(node).toEqual({
      kind: "text",
      runs: [{ text: "ok ", style: { color: "green", bold: true } }, { text: "more" }],
    });
  });

  it("box omits the layout field when none is given", () => {
    expect(box([text("a")])).toEqual({
      kind: "box",
      children: [{ kind: "text", runs: [{ text: "a" }] }],
    });
  });

  it("frame round-trips through JSON without losing fields", () => {
    const original: SceneFrame = frame(
      80,
      24,
      box(
        [
          text([run("status: ", { dim: true }), run("ok", { color: "green" })]),
          text("ready", "truncate"),
        ],
        { direction: "column", gap: 1, paddingX: 1 },
      ),
    );
    const round = JSON.parse(JSON.stringify(original)) as SceneFrame;
    expect(round).toEqual(original);
    expect(round.schemaVersion).toBe(1);
  });
});
