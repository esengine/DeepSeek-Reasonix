// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MockPort } from "../port/mock";
import { WorkbenchPanel } from "./WorkbenchPanel";

afterEach(() => { cleanup(); localStorage.clear(); });

const mount = () =>
  render(<WorkbenchPanel port={new MockPort()} tabs={[]} manual={false} shown scheme="light" changes={[]} onCloseManual={vi.fn()} onSurfaces={vi.fn()} onExternal={vi.fn()} />);

describe("the workbench start menu once the explorer is chosen", () => {
  it("is replaced by a prompt to pick a file, not left beside the tree it just opened", async () => {
    const { container } = mount();
    expect(container.querySelector(".workbench-launcher")).not.toBeNull();
    fireEvent.click(container.querySelector('[data-action="workbench.launch-files"]')!);
    expect(container.querySelector(".workbench-body")!.hasAttribute("data-files")).toBe(true);
    expect(container.querySelector(".workbench-launcher")).toBeNull();
    expect(await screen.findByText("从左侧选一个文件")).toBeTruthy();
  });

  it("comes back when the explorer is hidden again", () => {
    const { container } = mount();
    fireEvent.click(container.querySelector('[data-action="workbench.launch-files"]')!);
    fireEvent.click(container.querySelector('[aria-pressed="true"]')!);
    expect(container.querySelector(".workbench-launcher")).not.toBeNull();
  });
});
