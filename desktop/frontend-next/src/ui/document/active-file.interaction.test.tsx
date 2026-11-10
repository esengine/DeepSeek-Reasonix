// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { MockPort } from "../../port/mock";
import { WorkbenchPanel } from "../WorkbenchPanel";
import "../testkit";

afterEach(cleanup);

it("reports actual tab selection and clears it when the canvas is hidden or its file closes", async () => {
  const port = new MockPort(), onActiveFile = vi.fn();
  const props = { port, tabs: [], manual: false, shown: true, scheme: "light" as const, changes: [],
    workspaceRoot: "/project", onSurfaces: vi.fn(), onCloseManual: vi.fn(), onExternal: vi.fn(), onActiveFile };
  const view = render(<WorkbenchPanel {...props} fileRequest={{ path: "docs/one.md" }} />);
  await waitFor(() => expect(onActiveFile).toHaveBeenLastCalledWith("docs/one.md"));
  view.rerender(<WorkbenchPanel {...props} fileRequest={{ path: "other/one.md" }} />);
  await waitFor(() => expect(onActiveFile).toHaveBeenLastCalledWith("other/one.md"));
  view.rerender(<WorkbenchPanel {...props} />);
  fireEvent.click(screen.getAllByRole("button", { name: "one.md" })[0]);
  await waitFor(() => expect(onActiveFile).toHaveBeenLastCalledWith("docs/one.md"));
  fireEvent.click(screen.getByRole("button", { name: "文件" }));
  await waitFor(() => expect(onActiveFile).toHaveBeenLastCalledWith("docs/one.md"));
  fireEvent.click(screen.getByRole("button", { name: "新建标签" }));
  await waitFor(() => expect(onActiveFile).toHaveBeenLastCalledWith(undefined));
  fireEvent.click(screen.getByRole("button", { name: "新建标签" }));
  await waitFor(() => expect(onActiveFile).toHaveBeenLastCalledWith("docs/one.md"));
  fireEvent.click(screen.getAllByRole("button", { name: "关闭 one.md" })[0]);
  await waitFor(() => expect(onActiveFile).toHaveBeenLastCalledWith("other/one.md"));
  view.rerender(<WorkbenchPanel {...props} shown={false} />);
  await waitFor(() => expect(onActiveFile).toHaveBeenLastCalledWith(undefined));
});
