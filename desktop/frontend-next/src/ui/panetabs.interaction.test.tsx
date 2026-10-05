// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { PaneTabs, type TabView } from "./PaneTabs";
import type { RuntimeView } from "../port/hub";

afterEach(cleanup);

const tab = (id: string, live: boolean): TabView => ({
  rt: { id, root: "/w/" + id, name: id } as RuntimeView,
  title: id,
  run: live ? "running" : "idle",
  live,
});

function draw(tabs: TabView[]) {
  const onClose = vi.fn();
  const props = { active: tabs[0]?.rt.id ?? "", showRoot: false, onFocus: () => {}, onClose, onRename: () => {} };
  const view = render(<PaneTabs tabs={tabs} {...props} />);
  return {
    onClose,
    view,
    again: (next: TabView[]) => view.rerender(<PaneTabs tabs={next} {...props} />),
    strip: () => screen.queryByRole("tablist"),
    ask: () => screen.queryByRole("alertdialog"),
    closerOf: (id: string) =>
      within(document.querySelector(`[data-pane="${id}"]`) as HTMLElement).getByRole("button", { name: "关闭这个面板" }),
  };
}

const confirmBtn = () => within(screen.getByRole("alertdialog")).getByRole("button", { name: "关闭" });

describe("closing a pane that is still working", () => {
  it("asks nothing at all when nothing is running", async () => {
    const { onClose, closerOf, ask } = draw([tab("A", false), tab("B", false)]);
    await userEvent.click(closerOf("B"));
    expect(onClose).toHaveBeenCalledWith(["B"]);
    expect(ask()).toBeNull();
  });

  // The claim this cut is about. Replacing the strip loses the one thing the
  // reader needs to answer: which of them this is about.
  it("keeps the whole tab strip in place while it asks", async () => {
    const { closerOf, strip, ask } = draw([tab("A", false), tab("B", true), tab("C", false)]);
    await userEvent.click(closerOf("B"));
    expect(ask()).toBeTruthy();
    expect(strip()).toBeTruthy();
    expect(screen.getAllByRole("tab").map((el) => el.getAttribute("data-pane"))).toEqual(["A", "B", "C"]);
  });

  it("puts the keyboard inside the question it just asked", async () => {
    const { closerOf } = draw([tab("A", false), tab("B", true)]);
    await userEvent.click(closerOf("B"));
    expect(screen.getByRole("alertdialog").contains(document.activeElement)).toBe(true);
  });

  // Rule and mechanism in one: what was live when the question went up does not
  // decide anything. A pane that finished while the question was on screen is
  // closed without the sentence about stopping it.
  it("re-reads what is still running when the answer comes", async () => {
    const { onClose, again, closerOf } = draw([tab("A", false), tab("B", true)]);
    await userEvent.click(closerOf("B"));
    expect(screen.getByRole("alertdialog").textContent).toContain("仍在运行");

    again([tab("A", false), tab("B", false)]);
    await waitFor(() => expect(screen.getByRole("alertdialog").textContent).toContain("均已停止"));
    expect(screen.getByRole("alertdialog").textContent).not.toContain("仍在运行");

    await userEvent.click(confirmBtn());
    expect(onClose).toHaveBeenCalledWith(["B"]);
  });

  it("never hands back an id whose pane has gone", async () => {
    const { onClose, again } = draw([tab("A", false), tab("B", true), tab("C", true)]);
    fireEvent.contextMenu(document.querySelector('[data-pane="B"]') as HTMLElement);
    await userEvent.click(screen.getByRole("menuitem", { name: /全部关闭/ }));
    expect(screen.getByRole("alertdialog")).toBeTruthy();

    again([tab("A", false), tab("B", true)]);
    await userEvent.click(confirmBtn());
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(onClose).toHaveBeenCalledWith(["A", "B"]);
  });

  it("takes the question away when everything it was about is gone", async () => {
    const { again, closerOf, ask, strip } = draw([tab("A", false), tab("B", true)]);
    await userEvent.click(closerOf("B"));
    again([tab("A", false)]);
    await waitFor(() => expect(ask()).toBeNull());
    expect(strip()).toBeTruthy();
  });

  it("shrinks to what is left rather than asking about panes that went", async () => {
    const { again } = draw([tab("A", true), tab("B", true), tab("C", true)]);
    fireEvent.contextMenu(document.querySelector('[data-pane="A"]') as HTMLElement);
    await userEvent.click(screen.getByRole("menuitem", { name: /全部关闭/ }));
    expect(screen.getByRole("alertdialog").textContent).toContain("关闭 3 个面板");
    again([tab("A", true), tab("B", true)]);
    await waitFor(() => expect(screen.getByRole("alertdialog").textContent).toContain("关闭 2 个面板"));
  });

  it("cancels on Escape without closing anything, and gives the focus back", async () => {
    const { onClose, closerOf, ask } = draw([tab("A", false), tab("B", true)]);
    const x = closerOf("B");
    await userEvent.click(x);
    await userEvent.keyboard("{Escape}");
    await waitFor(() => expect(ask()).toBeNull());
    expect(onClose).not.toHaveBeenCalled();
    expect(document.activeElement).toBe(x);
  });

  it("cancels on a press outside it, and closes nothing", async () => {
    const { onClose, closerOf, ask } = draw([tab("A", false), tab("B", true)]);
    await userEvent.click(closerOf("B"));
    fireEvent.mouseDown(document.body);
    await waitFor(() => expect(ask()).toBeNull());
    expect(onClose).not.toHaveBeenCalled();
  });

  // One identity for closing panes, wherever it is answered: the confirmation
  // is a view, not a second kind of mutation.
  it("answers under the same action the close button asked with", async () => {
    const { closerOf } = draw([tab("A", true), tab("B", true)]);
    expect(closerOf("B").getAttribute("data-action")).toBe("pane.close");
    await userEvent.click(closerOf("B"));
    const go = confirmBtn();
    expect(go.getAttribute("data-action")).toBe("pane.close");
    expect(go.getAttribute("data-value")).toBe("one");
    expect(within(screen.getByRole("alertdialog")).getByRole("button", { name: "取消" }).hasAttribute("data-action")).toBe(false);
  });
});
