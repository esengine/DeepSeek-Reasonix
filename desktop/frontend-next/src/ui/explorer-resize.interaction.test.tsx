// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { ExplorerResize } from "./ExplorerResize";

afterEach(() => { cleanup(); localStorage.clear(); });

function mount() {
  const view = render(
    <div className="workbench-body">
      <aside className="workbench-explorer">
        <ExplorerResize />
      </aside>
    </div>,
  );
  const body = view.container.querySelector<HTMLElement>(".workbench-body")!;
  const aside = view.container.querySelector<HTMLElement>(".workbench-explorer")!;
  aside.getBoundingClientRect = () => ({ width: Number.parseFloat(body.style.getPropertyValue("--wb-tree-w")) || 220 }) as DOMRect;
  return { body, bar: screen.getByRole("separator", { name: "调整文件树宽度" }) };
}

describe("the file tree's width handle", () => {
  it("leaves the stylesheet's width alone until it is used", () => {
    const { body } = mount();
    expect(body.style.getPropertyValue("--wb-tree-w")).toBe("");
  });

  it("widens with the arrow key, remembers it, and clamps at the ends", () => {
    const { body, bar } = mount();
    fireEvent.keyDown(bar, { key: "ArrowRight", shiftKey: true });
    expect(body.style.getPropertyValue("--wb-tree-w")).toBe("268px");
    expect(localStorage.getItem("rx-wb-tree-w")).toBe("268");
    for (let i = 0; i < 40; i++) fireEvent.keyDown(bar, { key: "ArrowRight", shiftKey: true });
    expect(body.style.getPropertyValue("--wb-tree-w")).toBe("560px");
    for (let i = 0; i < 40; i++) fireEvent.keyDown(bar, { key: "ArrowLeft", shiftKey: true });
    expect(body.style.getPropertyValue("--wb-tree-w")).toBe("150px");
  });

  it("restores the remembered width on the next mount", () => {
    localStorage.setItem("rx-wb-tree-w", "333");
    expect(mount().body.style.getPropertyValue("--wb-tree-w")).toBe("333px");
  });

  it("returns to the default on Home and on a double click", () => {
    const { body, bar } = mount();
    fireEvent.keyDown(bar, { key: "ArrowRight" });
    fireEvent.keyDown(bar, { key: "Home" });
    expect(body.style.getPropertyValue("--wb-tree-w")).toBe("");
    expect(localStorage.getItem("rx-wb-tree-w")).toBeNull();
    fireEvent.keyDown(bar, { key: "ArrowRight" });
    fireEvent.doubleClick(bar);
    expect(body.style.getPropertyValue("--wb-tree-w")).toBe("");
  });

  it("follows a pointer drag and keeps the width where it was released", () => {
    const { body, bar } = mount();
    bar.setPointerCapture = () => {};
    fireEvent.pointerDown(bar, { button: 0, pointerId: 1, clientX: 220 });
    fireEvent.pointerMove(bar, { pointerId: 1, clientX: 300 });
    expect(body.style.getPropertyValue("--wb-tree-w")).toBe("300px");
    fireEvent.pointerUp(bar, { pointerId: 1, clientX: 300 });
    expect(localStorage.getItem("rx-wb-tree-w")).toBe("300");
    fireEvent.pointerMove(bar, { pointerId: 1, clientX: 500 });
    expect(body.style.getPropertyValue("--wb-tree-w")).toBe("300px");
  });

  it("ignores a stored width that is not a number", () => {
    localStorage.setItem("rx-wb-tree-w", "wide");
    expect(mount().body.style.getPropertyValue("--wb-tree-w")).toBe("");
  });
});
