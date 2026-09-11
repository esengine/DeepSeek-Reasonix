// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { PaneNav } from "./PaneNav";

afterEach(cleanup);

const draw = (over: Partial<Parameters<typeof PaneNav>[0]> = {}) => {
  const onPick = vi.fn();
  const r = render(
    <PaneNav view="flow" onPick={onPick} done={3} steps={7} nodes={12} rows={186} {...over} />,
  );
  return { onPick, ...r };
};

// The bar's own children, which is what "standing" means: a control reachable
// only after opening a menu does not occupy the reader's attention.
const standing = (c: HTMLElement) =>
  [...c.querySelectorAll(".tabs > .vtabs > button, .tabs > .picker > button")].map((b) =>
    (b.textContent ?? "").replace(/▾/g, "").trim(),
  );

describe("what the pane's bar keeps a permanent slot for", () => {
  it("stands three purposes, not five destinations", () => {
    const { container } = draw();
    expect(standing(container)).toEqual(["对话", "任务3/7", "运行详情"]);
  });

  // The sabotage this gate exists for: someone finds the menu a click too many
  // and puts 时间线 / 事件轨迹 / 运行图 back on the bar. Without this the whole
  // change is a CSS tidy-up that survives until the first person who disagrees.
  it("refuses to let a diagnostic view take a slot on the bar again", () => {
    const { container } = draw();
    for (const name of ["时间线", "事件轨迹", "运行图"]) {
      expect(standing(container), `${name} belongs to the detail menu, not the bar`).not.toContain(name);
    }
    // And not by any other route into the bar either.
    expect(container.querySelector(".vtabs")?.textContent).not.toMatch(/时间线|事件轨迹|运行图/);
  });

  // PaneTabs keeps the travelling marker. Two navigations drawing the same one
  // is what made the hierarchy unreadable, and it is the half of this change a
  // stylesheet edit could quietly undo.
  it("draws no travelling marker of its own", () => {
    const { container } = draw();
    expect(container.querySelector(".tabmark")).toBeNull();
  });
});

describe("what the bar counts", () => {
  // 347 transcript rows was the largest number on the bar and changed nothing
  // anyone does next.
  it("drops the transcript's item count", () => {
    const { container } = draw();
    expect(container.querySelector('[data-value="flow"]')?.textContent).toBe("对话");
  });

  it("keeps task progress, which is the count that was worth having", () => {
    const { container } = draw();
    expect(container.querySelector('[data-value="task"] .n')?.textContent).toBe("3/7");
  });

  it("says nothing about tasks before a plan exists", () => {
    const { container } = draw({ steps: 0, done: 0 });
    expect(container.querySelector('[data-value="task"] .n')).toBeNull();
  });

  // The diagnostic counts are not lost, only moved out of standing attention.
  it("moves the diagnostic counts into the menu", async () => {
    draw();
    await userEvent.click(screen.getByRole("button", { name: /运行详情/ }));
    const row = (name: string) => screen.getByRole("menuitem", { name: new RegExp(name) }).textContent;
    expect(row("时间线")).toContain("12");
    expect(row("事件轨迹")).toContain("186");
  });
});

describe("what the detail menu offers", () => {
  it("keeps all three diagnostic views", async () => {
    draw();
    await userEvent.click(screen.getByRole("button", { name: /运行详情/ }));
    expect(screen.getAllByRole("menuitem").map((n) => n.textContent?.replace(/\d+$/, "").trim()))
      .toEqual(["时间线", "事件轨迹", "运行图"]);
  });

  // The same rule the pane already follows when a run loses its graph. A
  // disabled row would be a control for a view that does not exist.
  it("does not offer a timeline there is no graph to draw", async () => {
    draw({ nodes: 0 });
    await userEvent.click(screen.getByRole("button", { name: /运行详情/ }));
    const names = screen.getAllByRole("menuitem").map((n) => n.textContent);
    expect(names.some((n) => n?.includes("时间线"))).toBe(false);
    expect(names.some((n) => n?.includes("事件轨迹"))).toBe(true);
    expect(names.some((n) => n?.includes("运行图"))).toBe(true);
  });

  it("hands back the pane's own view identity, not a name of its own", async () => {
    const { onPick } = draw();
    await userEvent.click(screen.getByRole("button", { name: /运行详情/ }));
    await userEvent.click(screen.getByRole("menuitem", { name: /事件轨迹/ }));
    expect(onPick).toHaveBeenCalledWith("traj");
  });

  // The trigger is a disclosure and nothing else. Opening it onto the last view
  // looked at, or onto a default one, is a state machine nobody can see: the
  // reader asked what the choices are, not to be moved.
  it("opens and closes without changing the view", async () => {
    const { onPick } = draw();
    await userEvent.click(screen.getByRole("button", { name: /运行详情/ }));
    await userEvent.keyboard("{Escape}");
    expect(onPick).not.toHaveBeenCalled();
  });

  it("switches views from the bar by the same identities", async () => {
    const { onPick, container } = draw();
    await userEvent.click(container.querySelector('[data-value="task"]') as HTMLElement);
    expect(onPick).toHaveBeenCalledWith("task");
  });
});

// What the trigger says it is, with the chevron taken off.
const triggerText = (c: HTMLElement) =>
  (c.querySelector(".tab.more")?.textContent ?? "").replace(/▾/g, "").trim();

describe("a view reached through the menu still has a name on screen", () => {
  // A page whose title lives only in a closed menu is a page the reader cannot
  // name — the failure mode of hiding navigation. Each of these is a first
  // render at that view, which is also how a session restored into one arrives:
  // the projection has no memory to consult, so what it draws is the identity
  // the pane handed it or nothing.
  for (const [view, label] of [["line", "时间线"], ["traj", "事件轨迹"], ["graph", "运行图"]] as const) {
    it(`carries ${label} alone, not the category it was reached through`, () => {
      const { container } = draw({ view });
      expect(triggerText(container)).toBe(label);
    });
  }

  it("names the category only while the reader is outside all three", () => {
    const { container } = draw({ view: "flow" });
    expect(triggerText(container)).toBe("运行详情");
  });

  it("marks the trigger as the one carrying the current view", () => {
    const { container } = draw({ view: "graph" });
    expect(container.querySelector(".tab.more.on")).toBeTruthy();
    expect(container.querySelector('[aria-selected="true"]')).toBeNull();
  });

  it("leaves the trigger unmarked while a bar view is showing", () => {
    const { container } = draw({ view: "flow" });
    expect(container.querySelector(".tab.more.on")).toBeNull();
    expect(container.querySelector('[data-value="flow"]')?.getAttribute("aria-selected")).toBe("true");
  });
});

describe("the menu is reachable without a pointer", () => {
  it("takes focus into the list on open and returns it on Escape", async () => {
    draw();
    const trigger = screen.getByRole("button", { name: /运行详情/ });
    await userEvent.click(trigger);
    expect(document.activeElement).toBe(screen.getAllByRole("menuitem")[0]);
    await userEvent.keyboard("{Escape}");
    expect(document.activeElement).toBe(trigger);
  });

  it("walks the rows with the arrow keys and takes one with Enter", async () => {
    const { onPick } = draw();
    await userEvent.click(screen.getByRole("button", { name: /运行详情/ }));
    await userEvent.keyboard("{ArrowDown}{Enter}");
    expect(onPick).toHaveBeenCalledWith("traj");
  });
});

// Reported from use: with a transcript on screen, the opened menu was drawn
// under the cards and its rows could not be clicked. Nothing in the transcript
// outranks it — the menu is z-index 30 and the highest card is 5 — so what was
// happening is that the menu's level was never the transcript's to compare
// against: a stacking context anywhere above the bar holds the whole menu at
// that ancestor's level, and the scroller, being the later sibling, paints over
// it and takes the presses. An entrance animation's fill transform and a card's
// paint containment both make one, which is why RewindControl's menu was moved
// out of the flow for the same reason.
//
// Rendering it outside the bar is what removes the dependency: the assertion is
// that it is not under the trigger any more, not that some particular ancestor
// is innocent today.
describe("where the detail menu is rendered", () => {
  it("is outside the bar, so no ancestor of the bar can hold its level down", async () => {
    const { container } = draw();
    await userEvent.click(screen.getByRole("button", { name: /运行详情/ }));

    const menu = document.querySelector(".menu[role='menu']:not([hidden])");
    expect(menu, "the menu opened").toBeTruthy();
    expect(container.contains(menu as Node), "and not inside the bar").toBe(false);
    expect(menu?.parentElement).toBe(document.body);
    // Its rows are reachable where it now lives.
    expect(document.querySelectorAll(".menu[role='menu']:not([hidden]) button.mi").length).toBeGreaterThan(0);
  });
});
