// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { PageIndex } from "./PageIndex";
import { NAV, SETTINGS, pageHome } from "./prefsnav";
import { pageIndexOf } from "./settingPageIndex";

afterEach(cleanup);

const entries = (section: string) => SETTINGS.filter((e) => !e.hidden && !e.within && pageHome(e.section) === section);

describe("the index at the top of a settings page", () => {
  it("names every setting the page holds, one button each, so folded-in ones have a visible way in", () => {
    const items = entries("session");
    render(<PageIndex items={items} onGo={() => {}} />);
    expect(screen.getByRole("navigation", { name: "本页包含" })).toBeTruthy();
    expect(screen.getAllByRole("button")).toHaveLength(items.length);
    for (const e of items) expect(document.querySelector(`[data-target="${e.anchor}"]`), e.anchor).toBeTruthy();
  });

  it("goes to the setting that was pressed, with the entry it was drawn from", async () => {
    const go = vi.fn();
    const items = entries("session");
    render(<PageIndex items={items} onGo={go} />);
    const peers = items.find((e) => e.anchor === "peers")!;
    await userEvent.click(document.querySelector(`[data-target="peers"]`) as HTMLElement);
    expect(go).toHaveBeenCalledWith(peers);
  });

  it("says nothing on a page too small to need it", () => {
    const { container } = render(<PageIndex items={entries("session").slice(0, 2)} onGo={() => {}} />);
    expect(container.innerHTML).toBe("");
  });
});

describe("which settings a page's index lists", () => {
  it("is every visible setting of the page that is not drawn inside another one", () => {
    const shown = pageIndexOf(SETTINGS, "session");
    expect(shown.map((e) => e.anchor)).toEqual(entries("session").map((e) => e.anchor));
  });

  it("leaves no setting off every page: each is listed, or sits inside one that is", () => {
    const homes = new Set(NAV.flatMap(([, items]) => items.map(([id]) => id as string)));
    const listed = new Set(SETTINGS.filter((e) => !e.hidden && !e.within && homes.has(pageHome(e.section))).map((e) => e.anchor));
    const lost = SETTINGS.filter((e) => !e.hidden && !listed.has(e.anchor) && !(e.within && listed.has(e.within))).map((e) => e.anchor);
    expect(lost).toEqual([]);
  });
});
