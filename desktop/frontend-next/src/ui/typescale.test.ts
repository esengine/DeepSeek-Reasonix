import { describe, expect, it } from "vitest";
// Not through import.meta.glob: vite's CSS handling intercepts ?raw there and
// hands back an empty string, which every rule below would then pass.
import css from "../styles/app.css?raw";

// Half a pixel is not a level. The interface carried 24 sizes with 442 of its
// declarations inside the 10–11.5px band, so nothing read as one step above
// anything — only as slightly different, everywhere. Six steps replace them,
// and this keeps a seventh from being typed in by hand.
describe("a size comes from the scale", () => {
  /** The size slot of every type declaration: before the `/line-height`. */
  const sizes = (): { where: string; slot: string }[] =>
    [...css.matchAll(/(?<![\w-])(font-size|font):\s*([^;}]+)/g)].map((m) => ({
      where: m[1],
      slot: m[2].split("/")[0],
    }));

  const RAW = /(?<![\w.-])(\d+(?:\.\d+)?)px(?![\w-])/g;
  // A length inside calc() against the reading token adjusts that token; it is
  // not a size of its own, and reading it as one puts 1.5px under the floor.
  const typedByHand = (slot: string) => !slot.includes("calc(") && !slot.includes("var(--read)");

  it("never types one into the interface", () => {
    // 24px and up is a brand moment — a splash wordmark is one size for one
    // thing, not a step other text is measured against.
    const typed = sizes().flatMap(({ where, slot }) =>
      [...slot.matchAll(RAW)]
        .filter((m) => Number(m[1]) < 24)
        .filter(() => typedByHand(slot))
        .map((m) => `${where}: …${m[0]}…`),
    );
    expect([...new Set(typed)], "hand-typed sizes: the scale says each one once").toEqual([]);
  });

  it("covers the type in the stylesheet", () => {
    // Guards the guard: a renamed property would leave the rule above passing
    // on an empty list.
    expect(sizes().length).toBeGreaterThan(300);
  });

  // The exemption above is what let the reading column keep a flat ladder: the
  // four heading levels an answer is written with were +1.5 / +.5 / 0 / -.5
  // pixels around the body, which is the same "slightly different, everywhere"
  // the scale was introduced to end — in the one column that is actually read
  // a paragraph at a time. A step is a ratio here, because the body size is a
  // setting and the levels have to hold at every value of it.
  it("sets the reading column's headings a step apart", () => {
    const step = (tag: string) => {
      const m = css.match(new RegExp(`\\.md ${tag} \\{ font-size: (?:calc\\(var\\(--read\\) \\* ([\\d.]+)\\)|var\\(--read\\))`));
      expect(m, `.md ${tag} declares its size against --read`).toBeTruthy();
      return m![1] ? Number(m![1]) : 1;
    };
    const ladder = [step("h1"), step("h2"), step("h3"), step("h4")];
    expect(ladder[0], "the top level outweighs the body it sits over").toBeGreaterThanOrEqual(1.25);
    for (let i = 1; i < ladder.length; i++) {
      expect(ladder[i - 1] / ladder[i], `h${i} over h${i + 1}`).toBeGreaterThanOrEqual(1.05);
    }
  });

  it("keeps the floor at the step the scale defines", () => {
    // Han strokes lose their counters before Latin does, and AA's large-text
    // exemption starts at 18.66px — nothing in the interface has ever been
    // inside it. The smallest step is the floor, so no rule may undercut it
    // by reaching for a raw value.
    const under = sizes().flatMap(({ slot }) =>
      [...slot.matchAll(RAW)]
        .filter((m) => Number(m[1]) < 11)
        .filter(() => typedByHand(slot))
        .map((m) => m[0]),
    );
    expect([...new Set(under)], "a size below the scale's floor").toEqual([]);
  });
});
