// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, renderHook, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { langOf } from "./fileKind";
import { leave, slideAway, useEnteringTabs, useOpenSlide } from "./workbench_motion";
import { WorkbenchTabOverflow } from "./WorkbenchTabOverflow";

afterEach(() => { cleanup(); vi.useRealTimers(); });

describe("tabs that move, as the WB-Frame board draws them", () => {
  it("brings in a tab that opened after the strip was drawn, and not the ones drawn with it", () => {
    vi.useFakeTimers();
    const { result, rerender } = renderHook(({ keys }) => useEnteringTabs(keys), { initialProps: { keys: ["file:a", "file:b"] } });
    expect([...result.current]).toEqual([]);
    rerender({ keys: ["file:a", "file:b", "browser:t1"] });
    expect([...result.current]).toEqual(["browser:t1"]);
    act(() => void vi.advanceTimersByTime(300));
    expect([...result.current]).toEqual([]);
  });

  it("closes at once where there is nothing to animate with", () => {
    const then = vi.fn();
    leave(document.createElement("div"), then);
    expect(then).toHaveBeenCalled();
  });

  it("colours Go, Python and sheets apart and leaves the rest alone", () => {
    expect(["a/b.go", "x.py", "q3.xlsx", "data.csv", "README.md"].map(langOf)).toEqual(["go", "python", "sheet", "sheet", undefined]);
  });

  it("lists the tabs the strip cannot show as +n, and picks one", async () => {
    const onPick = vi.fn();
    render(<WorkbenchTabOverflow tabs={[{ key: "file:a", label: "a.go" }, { key: "file:b", label: "b.go" }, { key: "file:c", label: "c.go" }]} onPick={onPick} />);
    await userEvent.click(screen.getByRole("button", { name: /\+3/ }));
    await userEvent.click(screen.getByRole("menuitem", { name: "b.go" }));
    expect(onPick).toHaveBeenCalledWith("file:b");
    expect(screen.queryByRole("menu")).toBeNull();
  });

  it("draws nothing while every tab fits", () => {
    const { container } = render(<WorkbenchTabOverflow tabs={[]} onPick={vi.fn()} />);
    expect(container.firstChild).toBeNull();
  });

  it("slides the workbench in when it is brought out, and not when it is drawn already out", () => {
    const el = document.createElement("section");
    const animate = vi.fn(() => ({ finished: Promise.resolve() }));
    Object.assign(el, { animate });
    const ref = { current: el };
    const { rerender } = renderHook(({ shown }) => useOpenSlide(ref, shown), { initialProps: { shown: true } });
    expect(animate).not.toHaveBeenCalled();
    rerender({ shown: false });
    rerender({ shown: true });
    expect(animate).toHaveBeenCalledTimes(1);
  });

  it("puts the workbench away once it has slid out", async () => {
    const el = document.createElement("section");
    let finish!: () => void;
    Object.assign(el, { animate: () => ({ finished: new Promise<void>((done) => { finish = done; }) }) });
    const then = vi.fn();
    slideAway(el, then);
    expect(then).not.toHaveBeenCalled();
    await act(async () => finish());
    expect(then).toHaveBeenCalled();
  });
});
