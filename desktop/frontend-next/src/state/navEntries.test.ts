// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from "vitest";
import { arranged, navLayout, onNavLayoutChange, resetNavLayout, setNavLayout } from "./navEntries";

const ALL = ["usage", "files", "tools", "ext", "memory", "remote"].map((k) => k);

beforeEach(() => localStorage.clear());

describe("which destinations the rail and the sidebar offer, and in what order", () => {
  it("is the declared order with nothing hidden until this machine chooses otherwise", () => {
    expect(arranged(ALL, navLayout())).toEqual(ALL);
  });

  it("puts the chosen order first and keeps the rest in their declared order", () => {
    setNavLayout({ order: ["remote", "files"], hidden: [] });
    expect(arranged(ALL, navLayout())).toEqual(["remote", "files", "usage", "tools", "ext", "memory"]);
  });

  it("leaves out what is hidden, wherever it sits", () => {
    setNavLayout({ order: ["remote"], hidden: ["files", "remote"] });
    expect(arranged(ALL, navLayout())).toEqual(["usage", "tools", "ext", "memory"]);
  });

  it("ignores names no destination has, so an older or newer build's choice cannot break the list", () => {
    setNavLayout({ order: ["gone", "ext"], hidden: ["gone-too"] });
    expect(arranged(ALL, navLayout())).toEqual(["ext", "usage", "files", "tools", "memory", "remote"]);
  });

  it("reads anything unreadable as the default", () => {
    for (const raw of ["", "{", "[]", "null", '{"order":"x","hidden":3}', '{"order":[1],"hidden":[{}]}']) {
      localStorage.setItem("rx-nav-entries", raw);
      expect(arranged(ALL, navLayout()), raw).toEqual(ALL);
    }
  });

  it("returns the same object until something changes, so a subscriber does not loop", () => {
    setNavLayout({ order: ["ext"], hidden: [] });
    expect(navLayout()).toBe(navLayout());
  });

  it("tells subscribers when the choice changes or is reset", () => {
    let heard = 0;
    const stop = onNavLayoutChange(() => heard++);
    setNavLayout({ order: [], hidden: ["memory"] });
    resetNavLayout();
    stop();
    expect(heard).toBe(2);
    expect(arranged(ALL, navLayout())).toEqual(ALL);
  });
});
