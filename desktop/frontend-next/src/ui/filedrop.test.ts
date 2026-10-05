import { describe, expect, it } from "vitest";
import { createDropRouter, type Dropped } from "./filedrop";

const GRACE = 250;

// A clock the test drives, so the two channels can be made to arrive in either
// order — which is the whole subject, and which no amount of waiting on a real
// timer would pin down.
function harness() {
  let now = 1000;
  const timers: { at: number; fn: () => void }[] = [];
  const took: { zone: string; drop: Dropped }[] = [];
  const router = createDropRouter<string>((zone, drop) => took.push({ zone, drop }), {
    now: () => now,
    wait: (fn, ms) => timers.push({ at: now + ms, fn }),
    grace: GRACE,
  });
  const tick = (ms: number) => {
    now += ms;
    for (const t of timers.splice(0)) {
      if (t.at <= now) t.fn();
      else timers.push(t);
    }
  };
  return { router, took, tick };
}

const file = (name: string) => ({ name }) as File;

describe("drop router", () => {
  // Windows: the paths take a round trip through the shell, so the DOM event
  // lands first and the drop waits for them.
  it("answers with paths that arrive after the drop", () => {
    const { router, took, tick } = harness();
    router.over("composer");
    router.dropped("composer", [file("a.png")]);
    expect(took).toHaveLength(0);
    router.paths(["/Users/me/a.png"]);
    expect(took).toEqual([{ zone: "composer", drop: { paths: ["/Users/me/a.png"], files: [file("a.png")] } }]);
    tick(GRACE * 2);
    expect(took).toHaveLength(1);
  });

  // macOS: the native handler posts the paths before WebKit dispatches the
  // drop. The same drop must not be delivered twice for arriving backwards.
  it("answers once when the paths arrive before the drop", () => {
    const { router, took, tick } = harness();
    router.over("composer");
    router.paths(["/Users/me/a.png"]);
    expect(took).toHaveLength(1);
    router.dropped("composer", [file("a.png")]);
    tick(GRACE * 2);
    expect(took).toHaveLength(1);
  });

  // A browser tab, or a drag carrying a promise rather than a file on disk.
  // Nothing happening at all is what makes a drop look broken.
  it("settles for the bytes when no path ever comes", () => {
    const { router, took, tick } = harness();
    router.over("composer");
    router.dropped("composer", [file("a.png")]);
    tick(GRACE);
    expect(took).toEqual([{ zone: "composer", drop: { paths: [], files: [file("a.png")] } }]);
  });

  // The shell reports every drop in the window, including one that landed on
  // nothing. Delivering that to whichever zone subscribed first is how a file
  // dropped on the transcript ends up attached to a pane nobody was looking at.
  it("drops paths that belong to no zone", () => {
    const { router, took } = harness();
    router.over(null);
    router.paths(["/Users/me/a.png"]);
    expect(took).toHaveLength(0);
  });

  // Two panes are two zones, and the one under the pointer is the one that
  // asked for the file.
  it("delivers to the zone the drop landed on, not the one hovered before it", () => {
    const { router, took } = harness();
    router.over("left");
    router.over("right");
    router.dropped("right", []);
    router.paths(["/Users/me/a.png"]);
    expect(took).toHaveLength(1);
    expect(took[0].zone).toBe("right");
  });

  // A zone that unmounts between the drop and its paths — a pane closed, a
  // screen swapped — must not be delivered to.
  it("forgets a zone that went away mid-drop", () => {
    const { router, took, tick } = harness();
    router.over("composer");
    router.dropped("composer", [file("a.png")]);
    router.forget("composer");
    router.paths(["/Users/me/a.png"]);
    tick(GRACE * 2);
    expect(took).toHaveLength(0);
  });
});
