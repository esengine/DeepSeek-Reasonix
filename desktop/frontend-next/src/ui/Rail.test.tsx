// @vitest-environment jsdom
import { cleanup, fireEvent, render, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import "./testkit";
import { Rail, railLocalY, type RailMark } from "./Rail";

afterEach(cleanup);

const mark = (id: string, at: number, text: string, msgIndex = at): RailMark => ({
  id, at, text, msgIndex, block: 0, within: 0, of: 1, files: 0,
});

describe("conversation overview rail", () => {
  it("maps the pointer into layout coordinates under compact zoom", () => {
    // A 600px layout rail is only 540px tall on screen at 90% zoom. Its visual
    // centre must still address the 300px mark, not the unscaled 270px one.
    expect(railLocalY(100 + 270, 100, 540, 600)).toBe(300);
    expect(railLocalY(100 + 300, 100, 600, 600)).toBe(300);
  });

  it("keeps sparse messages in a compact ordered fisheye", async () => {
    const root = document.createElement("div");
    const flow = document.createElement("div");
    Object.defineProperties(root, {
      clientHeight: { configurable: true, value: 600 },
      scrollHeight: { configurable: true, value: 2400 },
      scrollTop: { configurable: true, writable: true, value: 0 },
    });
    const marks = [mark("first", 0, "第一条"), mark("last", 99, "最后一条")];
    const view = render(
      <Rail
        marks={marks} total={100}
        scroll={{ current: root }} flow={{ current: flow }}
        onJump={vi.fn()} bound scope="test"
      />,
    );

    const buttons = view.getAllByRole("button");
    await waitFor(() => expect(buttons[0].style.top).toBe("295.5px"));
    expect(buttons[1].style.top).toBe("304.5px");
    expect(buttons[0].getAttribute("aria-label")).toBe("定位到你说的：第一条");
  });
});

describe("a rail longer than it can show", () => {
  const rail = async (count: number, onJump = vi.fn()) => {
    const root = document.createElement("div");
    Object.defineProperties(root, {
      clientHeight: { configurable: true, value: 600 },
      scrollHeight: { configurable: true, value: 90000 },
      scrollTop: { configurable: true, writable: true, value: 0 },
    });
    const marks = Array.from({ length: count }, (_, i) => mark(`m${i}`, i, `message ${i}`));
    const view = render(
      <Rail marks={marks} total={count} scroll={{ current: root }} flow={{ current: document.createElement("div") }} onJump={onJump} bound scope="test" />,
    );
    await waitFor(() => {
      const drawn = view.container.querySelectorAll<HTMLElement>(".srail-m");
      expect(drawn.length).toBeGreaterThan(0);
      expect(drawn[drawn.length - 1].style.top).not.toBe("0px");
    });
    return { view, marks, onJump };
  };

  it("draws one mark per distinguishable position, not one per message", async () => {
    const { view } = await rail(2000);
    const drawn = view.container.querySelectorAll(".srail-m").length;
    expect(drawn).toBeLessThanOrEqual(300);
    expect(drawn).toBeGreaterThan(50);
  });

  it("draws every mark while they are far enough apart to tell apart", async () => {
    const { view } = await rail(40);
    expect(view.container.querySelectorAll(".srail-m").length).toBe(40);
  });

  it("still addresses every message by position: the pointer lands on the nearest one", async () => {
    const { view, onJump } = await rail(2000);
    const nav = view.container.querySelector(".srail") as HTMLElement;
    nav.getBoundingClientRect = () => ({ top: 0, height: 600, left: 0, width: 20, right: 20, bottom: 600, x: 0, y: 0, toJSON: () => ({}) });
    fireEvent.mouseMove(nav, { clientY: 300 });
    fireEvent.click(nav);
    expect(onJump).toHaveBeenCalledTimes(1);
    const picked = Number((onJump.mock.calls[0][0] as RailMark).id.slice(1));
    // 2000 questions in 600px are folded into runs, so a press lands on the run
    // that holds the thousandth rather than on the thousandth itself.
    expect(Math.abs(picked - 1000)).toBeLessThanOrEqual(40);
  });
});

describe("a star belongs to the question, not the row id", () => {
  const two = (a: string, b: string): RailMark[] => [mark(a, 0, "第一条"), mark(b, 1, "第二条")];

  const draw = (marks: RailMark[], scope: string) => {
    const root = document.createElement("div");
    const flow = document.createElement("div");
    Object.defineProperties(root, {
      clientHeight: { configurable: true, value: 600 },
      scrollHeight: { configurable: true, value: 2400 },
      scrollTop: { configurable: true, writable: true, value: 0 },
    });
    return render(
      <Rail marks={marks} total={marks.length} scroll={{ current: root }} flow={{ current: flow }}
        onJump={vi.fn()} bound scope={scope} />,
    );
  };

  it("keeps the star on the same question after a remount with fresh ids", async () => {
    localStorage.clear();
    const first = draw(two("i1", "i2"), "s1");
    fireEvent.contextMenu(first.getAllByRole("button")[0]);
    await waitFor(() => expect(first.getAllByRole("button")[0].getAttribute("data-starred")).toBe(""));
    first.unmount();

    // A reload gives every row a new id; the star must follow the position.
    const again = draw(two("j1", "j2"), "s1");
    await waitFor(() => expect(again.getAllByRole("button")[0].getAttribute("data-starred")).toBe(""));
    expect(again.getAllByRole("button")[1].getAttribute("data-starred")).toBeNull();
  });

  it("does not carry a star into another conversation", async () => {
    localStorage.clear();
    const first = draw(two("i1", "i2"), "s1");
    fireEvent.contextMenu(first.getAllByRole("button")[0]);
    await waitFor(() => expect(first.getAllByRole("button")[0].getAttribute("data-starred")).toBe(""));
    first.unmount();

    const other = draw(two("i1", "i2"), "s2");
    expect(other.getAllByRole("button")[0].getAttribute("data-starred")).toBeNull();
  });

  it("keeps the star when a notice shifts the rendered index", async () => {
    localStorage.clear();
    // Two user turns at rendered positions 0 and 1.
    const first = draw([mark("i1", 0, "第一条", 2), mark("i2", 1, "第二条", 3)], "s1");
    fireEvent.contextMenu(first.getAllByRole("button")[0]);
    await waitFor(() => expect(first.getAllByRole("button")[0].getAttribute("data-starred")).toBe(""));
    first.unmount();

    // A reload puts a notice above them: `at` moves to 1 and 2 while the logged
    // index stays 2 and 3, so the star follows the question, not the position.
    const again = draw([mark("j1", 1, "第一条", 2), mark("j2", 2, "第二条", 3)], "s1");
    await waitFor(() => expect(again.getAllByRole("button")[0].getAttribute("data-starred")).toBe(""));
    expect(again.getAllByRole("button")[1].getAttribute("data-starred")).toBeNull();
  });
});
