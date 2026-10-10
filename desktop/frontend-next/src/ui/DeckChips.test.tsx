// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render } from "@testing-library/react";
import "./testkit";
import { DeckChips } from "./DeckChips";
import type { JobEntry } from "../port/port";
import type { Task } from "./panels/Agents";

afterEach(cleanup);

const job = (over: Partial<JobEntry> = {}): JobEntry =>
  ({ id: "j1", label: "pnpm dev", status: "running", startedAt: Date.now(), ...over }) as JobEntry;

const task = (running: boolean, id = "t1"): Task =>
  ({ t: "tool", id, running, children: [], tool: { name: "task", args: '{"subagent_type":"Explore"}' } }) as unknown as Task;

const draw = (tasks: Task[], jobs: JobEntry[]) =>
  render(<DeckChips tasks={tasks} jobs={jobs} open="" onOpen={vi.fn()} />).container;

// These are readings about the turn, so they live with the turn's other
// readings and open the way those do: the detail is part of the same object,
// not a panel somewhere else that a click has to go and find.
describe("the deck readings", () => {
  it("say nothing when nothing is running beside the conversation", () => {
    expect(draw([], []).querySelector(".studio-deck-anchor")).toBeNull();
  });

  it("carry their own detail rather than opening something elsewhere", () => {
    const box = draw([], [job()]);
    const anchor = box.querySelector(".studio-deck-anchor");
    expect(anchor?.querySelector('[data-action="deck.jobs"]')).not.toBeNull();
    // Inside the same anchor, which is what lets hover and focus reach it.
    expect(anchor?.querySelector(".studio-deck-pop")).not.toBeNull();
  });

  it("count only what is live, and read 0 when everything has ended", () => {
    expect(draw([], [job()]).querySelector('[data-action="deck.jobs"] b')?.textContent).toBe("1");
    const settled = draw([], [job({ status: "exited" })]);
    expect(settled.querySelector('[data-action="deck.jobs"] b')?.textContent).toBe("0");
    expect(settled.querySelector('[data-action="deck.jobs"]')?.hasAttribute("data-live")).toBe(false);
  });

  it("count sub-agents the same way: running ones on the chip, all of them in the popover", () => {
    const mix = [task(true, "a"), task(true, "b"), task(false, "c"), task(false, "d"), task(false, "e")];
    const box = draw(mix, []);
    expect(box.querySelector('[data-action="deck.agents"] b')?.textContent).toBe("2");
    expect(box.querySelector(".studio-deck-pop")?.textContent).toContain("共 5");
    expect(box.querySelectorAll(".studio-deck-pop .ag")).toHaveLength(5);
  });

  it("keeps the history in the popover when every sub-agent has finished", () => {
    const box = draw([task(false, "a"), task(false, "b")], []);
    expect(box.querySelector('[data-action="deck.agents"] b')?.textContent).toBe("0");
    expect(box.querySelector('[data-action="deck.agents"]')?.hasAttribute("data-live")).toBe(false);
    expect(box.querySelector(".studio-deck-pop")?.textContent).toContain("共 2");
  });

  it("does not count a failed or cancelled sub-agent as running", () => {
    const failed = { ...task(false, "f"), tool: { name: "task", args: "{}", err: "interrupted" } } as unknown as Task;
    const box = draw([failed, task(true, "g")], []);
    expect(box.querySelector('[data-action="deck.agents"] b')?.textContent).toBe("1");
    expect(box.querySelector(".studio-deck-pop")?.textContent).toContain("共 2");
  });

  it("moves up by one when another sub-agent starts", () => {
    const first = [task(true, "a"), task(false, "b")];
    const view = render(<DeckChips tasks={first} jobs={[]} open="" onOpen={vi.fn()} />);
    expect(view.container.querySelector('[data-action="deck.agents"] b')?.textContent).toBe("1");
    view.rerender(<DeckChips tasks={[...first, task(true, "c")]} jobs={[]} open="" onOpen={vi.fn()} />);
    expect(view.container.querySelector('[data-action="deck.agents"] b')?.textContent).toBe("2");
    expect(view.container.querySelector(".studio-deck-pop")?.textContent).toContain("共 3");
  });

  // Finished delegates stay reachable (their transcript opens from here), but
  // only a running one animates the reading.
  it("keeps finished delegates listed and animates only while one is running", () => {
    expect(draw([task(true)], []).querySelector('[data-action="deck.agents"]')?.hasAttribute("data-live")).toBe(true);
    const done = draw([task(false)], []).querySelector('[data-action="deck.agents"]');
    expect(done).not.toBeNull();
    expect(done?.hasAttribute("data-live")).toBe(false);
  });
});
