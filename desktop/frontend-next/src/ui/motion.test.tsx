// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render } from "@testing-library/react";
import "./testkit";
import { Transcript } from "./Transcript";
import type { Item } from "../state/session";
// Not through import.meta.glob: vite's CSS handling intercepts ?raw there and
// hands back an empty string, which every rule below would then pass.
import css from "../styles/app.css?raw";

afterEach(cleanup);

const user = (id: string, text: string): Item => ({ t: "user", id, text });

function draw(items: Item[], entering: string[], hidden = false, onPinned = () => {}) {
  const onEntered = vi.fn();
  const noop = async () => undefined as never;
  const scroll = { current: null as HTMLDivElement | null };
  const view = render(
    <Transcript
      items={items} entering={entering} onEntered={onEntered} revision={1}
      waiting={{}} scroll={scroll} hidden={hidden} onPinned={onPinned}
      jump={0} focus={null}
      onApprove={noop} onFullAccess={noop} onPlan={noop} onAnswer={noop} onForget={noop}
      onExtInvoke={() => {}} onExtSubmit={noop}
      checkpoints={new Map()} onPrepareRewind={noop} onCommitRewind={noop} onUndoRewind={noop}
      onPrepareFileRevert={noop} onCommitFileRevert={noop}
      needsProject={false} onOpenProject={() => {}} onKeepHere={() => {}}
    />,
  );
  const entered = () => [...document.querySelectorAll(".enterbox[data-enter]")].length;
  return { view, onEntered, entered, scroll };
}

describe("leaving the live edge", () => {
  it("does not offer back to latest for one small upward wheel gesture", () => {
    const onPinned = vi.fn();
    const { scroll } = draw([user("i1", "hi")], [], false, onPinned);
    const root = scroll.current!;
    let top = 400;
    Object.defineProperties(root, {
      scrollTop: { configurable: true, get: () => top, set: (value: number) => { top = value; } },
      scrollHeight: { configurable: true, value: 1000 },
      clientHeight: { configurable: true, value: 600 },
    });
    onPinned.mockClear();

    fireEvent.wheel(root, { deltaY: -80 });
    top = 330;
    fireEvent.scroll(root);
    expect(onPinned).not.toHaveBeenCalledWith(false);

    top = 180;
    fireEvent.scroll(root);
    expect(onPinned).toHaveBeenCalledWith(false);
  });
});

describe("a card's one entrance", () => {
  it("is marked on the card that has just arrived", () => {
    const { entered } = draw([user("i1", "hi")], ["i1"]);
    expect(entered()).toBe(1);
  });

  it("is spent by the render that drew it, not by the animation ending", () => {
    const { onEntered } = draw([user("i1", "hi")], ["i1"]);
    // No animation has run — jsdom has none, and reduced motion runs none
    // either. The debt is settled all the same.
    expect(onEntered).toHaveBeenCalledWith(["i1"]);
  });

  // What virtualization does: the same card, a new element, after the
  // projection has already spent the debt.
  it("is not given again when the same card mounts a second time", () => {
    const first = draw([user("i1", "hi")], ["i1"]);
    expect(first.entered()).toBe(1);
    cleanup();
    const again = draw([user("i1", "hi")], []);
    expect(again.entered()).toBe(0);
  });

  it("survives its own card re-rendering while it is still owed", () => {
    const { view, entered } = draw([user("i1", "hi")], ["i1"]);
    expect(entered()).toBe(1);
    // The projection spends the debt on the next commit; the mark has to stay
    // for this element, or a streaming answer loses its entrance halfway.
    view.rerender(view.container.firstChild ? (
      <Transcript
        items={[user("i1", "hi")]} entering={[]} onEntered={() => {}} revision={2}
        waiting={{}} scroll={{ current: null }} hidden={false} onPinned={() => {}}
        jump={0} focus={null}
        onApprove={(async () => undefined) as never} onFullAccess={(async () => undefined) as never} onPlan={(async () => undefined) as never}
        onAnswer={(async () => undefined) as never} onForget={(async () => undefined) as never}
        onExtInvoke={() => {}} onExtSubmit={(async () => undefined) as never}
        checkpoints={new Map()} onPrepareRewind={(async () => undefined) as never}
        onCommitRewind={(async () => undefined) as never} onUndoRewind={(async () => undefined) as never}
        onPrepareFileRevert={(async () => undefined) as never} onCommitFileRevert={(async () => undefined) as never}
        needsProject={false} onOpenProject={() => {}} onKeepHere={() => {}}
      />
    ) : null);
    expect(entered()).toBe(1);
  });

  // A pane that is not the one on screen still holds its transcript. Coming
  // back to it is not a fact arriving.
  it("is spent even while the pane is not the one being looked at", () => {
    const { onEntered } = draw([user("i1", "hi")], ["i1"], true);
    expect(onEntered).toHaveBeenCalledWith(["i1"]);
  });
});

// The rule underneath both of the above, held on the stylesheet itself: a
// selector that fires an entrance the moment an element exists has taken
// mount for a cause. Written here rather than left to review, because the
// tempting line is one character short of the correct one.
describe("mount is not a cause", () => {
  /** Rules that give this selector an entrance animation, by exact term. */
  const entrances = (term: string) =>
    css
      .split("\n")
      // Turning one off is not giving one: the reduced-motion block names the
      // same selectors to say they must not move.
      .filter((l) => l.includes("animation:") && !/animation:\s*none/.test(l) && !l.trimStart().startsWith("/*"))
      .filter((l) => l.split("{")[0].split(",").some((sel) => sel.trim() === term));

  it("does not animate a transcript card into being", () => {
    expect(entrances(".call"), "a card animates whenever one exists — including replays").toEqual([]);
  });

  it("does not animate a settings block into being", () => {
    expect(entrances(".grp"), "every block flies in on a section change, and nothing happened").toEqual([]);
  });

  it("does not replay an old card's inner pieces when virtualization remounts it", () => {
    for (const selector of [
      ".nest-bd .call", ".nest", ".ask", ".find", ".nest-ret",
      ".guard", ".apv", ".hl .cost", ".pip[data-settled]",
      ".pane[data-run=\"done\"] .rmark", ".pane[data-run=\"done\"] .rmark path",
    ]) {
      expect(entrances(selector), `${selector} moves merely because an old card mounted`).toEqual([]);
    }
  });

  it("moves inner results only while their live card owns an entrance", () => {
    for (const selector of [
      ".enterbox[data-enter] .nest-bd .call",
      ".enterbox[data-enter] .find",
      ".enterbox[data-enter] .nest-ret",
      ".enterbox[data-enter] .guard",
      ".enterbox[data-enter] .apv",
      ".enterbox[data-enter] .hl .cost",
    ]) {
      expect(entrances(selector).length, `${selector} lost its live entrance`).toBe(1);
    }
  });

  it("still animates the one that really did just arrive", () => {
    expect(entrances(".enterbox[data-enter] > .call").length).toBe(1);
    expect(entrances(".prefs-col").length).toBe(1);
  });
});

// One weight decides a duration, so a duration is never typed at the call site.
// Four speeds for one weight was the state this replaced: .12 / .14 / .15 / .16
// all meant "a line just changed", and the same change ran at four speeds in
// four corners — which is what reads as an interface that is not quite settled.
describe("a duration comes from a weight", () => {
  /** Every `transition:` declaration body, joined across the lines it spans. */
  const transitions = (): string[] => {
    const out: string[] = [];
    for (const m of css.matchAll(/\btransition:/g)) {
      let i = m.index + m[0].length;
      let depth = 0;
      for (; i < css.length; i++) {
        const c = css[i];
        if (c === "(") depth++;
        else if (c === ")") depth--;
        else if ((c === ";" || c === "}") && depth === 0) break;
      }
      out.push(css.slice(m.index + m[0].length, i));
    }
    return out;
  };

  it("never writes one into a transition", () => {
    const raw = transitions().filter((body) => /(?<![\w.-])\d*\.?\d+m?s(?![\w-])/.test(body));
    expect(raw, "hand-written durations: a weight token says it once for every use").toEqual([]);
  });

  it("never writes one into transition-duration either", () => {
    // .01ms is the reduced-motion kill switch: off is not a weight.
    const raw = [...css.matchAll(/transition-duration:\s*([^;}]+)/g)]
      .map((m) => m[1].trim())
      .filter((v) => /(?<![\w.-])\d*\.?\d+m?s(?![\w-])/.test(v.replace(/\b0s\b/g, "").replace(/\.01ms/g, "")));
    expect(raw, "a duration written beside the shorthand is still a duration").toEqual([]);
  });

  it("covers every transition in the stylesheet", () => {
    // Guards the guard: a renamed property or a moved file would leave the rule
    // above passing on an empty list.
    expect(transitions().length).toBeGreaterThan(120);
  });

  // --t-live is the tier that is not a weight. A fill reports a rate, and the
  // way it reports is by growing — so this tier drives a fill's own geometry
  // and nothing else. Acknowledging a click on it would take .6s to answer.
  it("keeps the live tier on a fill's geometry", () => {
    const GEOMETRY = /^\s*(width|height|flex-grow|flex-basis|stroke-dashoffset|stroke-dasharray)\s/;
    const misused = transitions()
      .filter((body) => body.includes("var(--t-live)"))
      .flatMap((body) => body.split(","))
      .filter((seg) => seg.includes("var(--t-live)") && !GEOMETRY.test(seg))
      .map((seg) => seg.trim());
    expect(misused, "the live tier reports work by growing; it does not answer actions").toEqual([]);
  });
});

describe("navigation focus", () => {
  it("marks a tool's identity without repainting its full output card", () => {
    expect(css).not.toMatch(/\.call\[data-hit\]\s*\{\s*animation:/);
    expect(css).toMatch(/\.call\[data-hit\].*\.hl\s*>\s*:is\(\.nm, \.arg\)\s*\{\s*animation:/);
  });
});
