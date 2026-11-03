// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import "./testkit";
import { Picker, type MenuItem } from "./Menu";

afterEach(cleanup);

const models: MenuItem[] = [{ value: "a", label: "alpha" }, { value: "b", label: "beta" }];

function mount(props: Partial<Parameters<typeof Picker>[0]> = {}) {
  const onPick = vi.fn();
  render(
    <Picker label="trigger" place="bottom" current="a" items={models} onPick={onPick} menuClassName="m"
      aside={(close) => <><button className="mi" data-aside="one" onClick={close}>one</button><button className="mi" data-aside="two" onClick={close}>two</button></>}
      {...props} />,
  );
  fireEvent.click(screen.getByRole("button", { name: "trigger" }));
  return onPick;
}

describe("a menu with a column beside its rows", () => {
  it("draws the column inside the same menu and marks the menu as having one", () => {
    mount();
    const menu = document.querySelector(".portmenu")!;
    expect(menu.classList.contains("has-aside")).toBe(true);
    expect(menu.querySelector(".menu-aside [data-aside='one']")).not.toBeNull();
  });

  it("closes from inside the column through the handle it is given", () => {
    mount();
    fireEvent.click(document.querySelector("[data-aside='two']")!);
    expect((document.querySelector(".portmenu") as HTMLElement).hidden).toBe(true);
  });

  it("stays open after a pick it was told to keep open, and closes after any other", () => {
    const onPick = mount({ stayOpen: (v: string) => v === "b" });
    fireEvent.click(document.querySelector('[data-value="b"]')!);
    expect(onPick).toHaveBeenCalledWith("b");
    expect((document.querySelector(".portmenu") as HTMLElement).hidden).toBe(false);
    fireEvent.click(document.querySelector('[data-value="a"]')!);
    expect((document.querySelector(".portmenu") as HTMLElement).hidden).toBe(true);
  });

  it("walks Up and Down inside one column and crosses with Left and Right", () => {
    mount();
    const rows = [...document.querySelectorAll<HTMLElement>(".portmenu > button.mi")];
    rows[0].focus();
    fireEvent.keyDown(rows[0], { key: "ArrowDown" });
    expect(document.activeElement).toBe(rows[1]);
    fireEvent.keyDown(rows[1], { key: "ArrowDown" });
    expect(document.activeElement).toBe(rows[1]);
    fireEvent.keyDown(rows[1], { key: "ArrowRight" });
    expect((document.activeElement as HTMLElement).dataset.aside).toBe("one");
    fireEvent.keyDown(document.activeElement!, { key: "ArrowDown" });
    expect((document.activeElement as HTMLElement).dataset.aside).toBe("two");
    fireEvent.keyDown(document.activeElement!, { key: "ArrowLeft" });
    expect(rows).toContain(document.activeElement);
  });
});
