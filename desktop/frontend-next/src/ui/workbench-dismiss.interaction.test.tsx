// @vitest-environment jsdom
import "./testkit";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { MockPort } from "../port/mock";
import { WorkbenchPanel } from "./WorkbenchPanel";

afterEach(cleanup);

function draw() {
  const onDismiss = vi.fn();
  const view = render(<WorkbenchPanel port={new MockPort()} tabs={[]} manual={false} shown scheme="light" changes={[]} onDismiss={onDismiss}
    onCloseManual={vi.fn()} onSurfaces={vi.fn()} onExternal={vi.fn()} />);
  return { onDismiss, ...view };
}

it("closing the workbench folds the explorer, so it opens again on the launcher", async () => {
  const { container, onDismiss } = draw();
  await userEvent.click(container.querySelector<HTMLElement>('[data-action="workbench.files"]')!);
  await waitFor(() => expect(container.querySelector(".workbench-body")?.hasAttribute("data-files")).toBe(true));
  await userEvent.click(container.querySelector<HTMLElement>('[data-action="workbench.dismiss"]')!);
  expect(onDismiss).toHaveBeenCalledOnce();
  expect(container.querySelector(".workbench-body")?.hasAttribute("data-files")).toBe(false);
  expect(screen.getByRole("group", { name: "开始" })).toBeTruthy();
});

it("closing the workbench keeps the files that are open", async () => {
  const { container } = draw();
  await userEvent.click(container.querySelector<HTMLElement>('[data-action="workbench.files"]')!);
  await userEvent.click(await screen.findByRole("button", { name: "README.md" }));
  await userEvent.click(container.querySelector<HTMLElement>('[data-action="workbench.dismiss"]')!);
  expect(screen.getByRole("tab", { name: "README.md" })).toBeTruthy();
});
