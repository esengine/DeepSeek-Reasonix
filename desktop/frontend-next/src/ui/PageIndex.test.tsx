// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { PageIndex } from "./PageIndex";
import { t } from "../i18n";
import { NAV, PAGE_CARDS, SETTINGS, pageHome } from "./prefsnav";
import { pageIndexOf } from "./settingPageIndex";

afterEach(cleanup);

const cards = (section: string) => pageIndexOf(SETTINGS.filter((e) => !e.hidden), section as never);

describe("the index at the top of a settings page", () => {
  it("names every card the page holds, one button each, so folded-in ones have a visible way in", () => {
    const items = cards("session");
    render(<PageIndex items={items} onGo={() => {}} />);
    expect(screen.getByRole("navigation", { name: "本页包含" })).toBeTruthy();
    expect(screen.getAllByRole("button")).toHaveLength(items.length);
    expect(items.map((i) => screen.getByRole("button", { name: t(i.title) }).textContent)).toEqual(["人设与预设", "计划与提问", "协作", "自动化"]);
  });

  it("goes to the first setting of the card that was pressed, with the entry it was drawn from", async () => {
    const go = vi.fn();
    const items = cards("session");
    render(<PageIndex items={items} onGo={go} />);
    const collab = items.find((i) => i.title === "settings.collaboration")!;
    await userEvent.click(document.querySelector(`[data-target="${collab.anchor}"]`) as HTMLElement);
    expect(go).toHaveBeenCalledWith(collab.entry);
  });

  it("says nothing on a page too small to need it", () => {
    const { container } = render(<PageIndex items={cards("session").slice(0, 1)} onGo={() => {}} />);
    expect(container.innerHTML).toBe("");
  });
});

describe("which cards a page's index lists", () => {
  it("lands each card on the first visible block it holds", () => {
    const hidden = SETTINGS.filter((e) => !e.hidden && e.anchor !== "plan-approval");
    const shown = pageIndexOf(hidden, "session");
    expect(shown.find((i) => i.title === "settings.plans_and_questions")?.anchor).toBe("ask-timeout");
  });

  it("drops a card none of whose blocks is visible", () => {
    const shown = pageIndexOf(SETTINGS.filter((e) => e.section !== "remote"), "network");
    expect(shown.map((i) => i.title)).toEqual(["common.network"]);
  });

  it("leaves no setting off every page: each sits in a card, or inside a block that does", () => {
    const homes = new Set(NAV.flatMap(([, items]) => items.map(([id]) => id as string)));
    const inCard = new Set(PAGE_CARDS.flatMap(([, , anchors]) => anchors));
    const lost = SETTINGS.filter((e) => !e.hidden && homes.has(pageHome(e.section)) && !inCard.has(e.within ?? e.anchor)).map((e) => e.anchor);
    expect(lost).toEqual([]);
  });

  it("puts every card on a page that exists and names only settings that exist", () => {
    const anchors = new Set(SETTINGS.map((e) => e.anchor));
    for (const [page, , list] of PAGE_CARDS) {
      expect(NAV.flatMap(([, items]) => items.map(([id]) => id as string))).toContain(page);
      for (const a of list) expect(anchors.has(a), a).toBe(true);
    }
  });
});
