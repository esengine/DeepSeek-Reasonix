// @vitest-environment jsdom
import { useState } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { arrowRadios } from "./tablist";

afterEach(cleanup);

it("wraps enabled radios with focus and click, leaving locked or unrelated keys alone", () => {
  const selected = vi.fn();
  function Radios({ locked = false }: { locked?: boolean }) {
    const [at, setAt] = useState("a");
    return <><div role="radiogroup" onKeyDown={arrowRadios}>
      {["a", "b", "c", "d"].map((id) => <button key={id} type="button" role="radio" aria-checked={at === id}
        tabIndex={at === id ? 0 : -1} disabled={locked || id === "b"}
        onClick={() => { setAt(id); selected(id); }}>{id}</button>)}
    </div><button type="button">outside</button></>;
  }
  const view = render(<Radios />);
  const radio = (name: string) => screen.getByRole<HTMLButtonElement>("radio", { name });
  radio("a").focus();
  for (const [key, name] of [["ArrowRight", "c"], ["ArrowDown", "d"], ["ArrowDown", "a"], ["ArrowLeft", "d"], ["ArrowUp", "c"], ["ArrowUp", "a"]]) {
    expect(fireEvent.keyDown(document.activeElement!, { key })).toBe(false);
    expect(document.activeElement).toBe(radio(name));
    expect(radio(name).getAttribute("aria-checked")).toBe("true");
    expect(radio(name).tabIndex).toBe(0);
    expect(selected).toHaveBeenLastCalledWith(name);
  }
  expect(selected.mock.calls.map(([name]) => name)).toEqual(["c", "d", "a", "d", "c", "a"]);
  for (const modifiers of [{ altKey: true }, { ctrlKey: true }, { metaKey: true }]) {
    expect(fireEvent.keyDown(radio("a"), { key: "ArrowRight", ...modifiers })).toBe(true);
  }
  for (const key of ["Enter", " ", "Home", "End", "Escape", "x"]) expect(fireEvent.keyDown(radio("a"), { key })).toBe(true);
  expect(selected).toHaveBeenCalledTimes(6);
  expect(document.activeElement).toBe(radio("a"));
  screen.getByRole("button", { name: "outside" }).focus();
  expect(fireEvent.keyDown(screen.getByRole("radiogroup"), { key: "ArrowRight" })).toBe(true);
  expect(selected).toHaveBeenCalledTimes(6);
  view.rerender(<Radios locked />);
  expect(fireEvent.keyDown(radio("a"), { key: "ArrowRight" })).toBe(true);
  expect(selected).toHaveBeenCalledTimes(6);
  expect(radio("a").getAttribute("aria-checked")).toBe("true");
});
