// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { RailSearch } from "./railsearch";
import { RemoteHosts } from "./RemoteHosts";
import type { RemoteHost } from "../port/remote";
import type { RuntimeView, TreeWorkspace } from "../port/hub";

afterEach(cleanup);

const hosts: RemoteHost[] = [
  { name: "gpu", target: "ada@10.0.0.4", status: "connected", workspaces: ["/work/project"] },
  { name: "build", target: "ci@10.0.0.9", status: "stopped", workspaces: ["/work/project"] },
];
const trees: Record<string, TreeWorkspace[]> = {
  gpu: [{ root: "/work/project", name: "project", sessions: [{ path: "/s/a", name: "GPU task" }] }],
  build: [{ root: "/work/project", name: "project", sessions: [{ path: "/s/a", name: "Build task", archived: true }] }],
};

function draw(scope: "all" | "live" | "archived" | "pinned" = "all", runtimes: RuntimeView[] = [], books = trees) {
  const onOpen = vi.fn().mockResolvedValue(undefined);
  const readTree = vi.fn().mockResolvedValue(undefined);
  const reload = vi.fn().mockResolvedValue(undefined);
  render(<RailSearch><RemoteHosts hub={{} as never} hosts={hosts} runtimes={runtimes} active=""
    onOpen={onOpen} onFocus={() => {}} reload={reload} trees={books}
    reloadTrees={async () => {}} onClose={async () => {}} liveIds={(ids) => ids}
    readTree={readTree} onError={() => {}} scope={scope} /></RailSearch>);
  return { onOpen, readTree, reload };
}

it("renders remote projects directly at level one, with host names only as metadata", () => {
  draw();
  expect(screen.queryByText("远程机器")).toBeNull();
  expect(document.querySelector(".machrow")).toBeNull();
  const projects = document.querySelectorAll(".wsrow");
  expect(projects).toHaveLength(2);
  for (const project of projects) expect(project.getAttribute("aria-level")).toBe("1");
  expect(screen.getByRole("treeitem", { name: /GPU task/ }).getAttribute("aria-level")).toBe("2");
  expect(screen.getByText("已断开")).toBeTruthy();
});

it("routes identical project and session paths to their owning SSH host", async () => {
  const { onOpen } = draw();
  await userEvent.click(screen.getByText("GPU task"));
  expect(onOpen).toHaveBeenCalledWith("gpu", "/work/project", "/s/a");
  await userEvent.click(screen.getByRole("button", { name: "在 project · build 下新建会话" }));
  expect(onOpen).toHaveBeenCalledWith("build", "/work/project", undefined);
});

it("folds projects independently even when roots match across hosts, and search reveals them", async () => {
  draw();
  await userEvent.click(document.querySelectorAll(".wsrow")[0]!);
  expect(screen.queryByText("GPU task")).toBeNull();
  expect(document.querySelectorAll(".wsrow")[1]!.getAttribute("aria-expanded")).toBeNull();
  await userEvent.type(screen.getByRole("searchbox", { name: "搜索会话 / 项目" }), "GPU task");
  expect(screen.getByText("GPU task")).toBeTruthy();
  expect(document.querySelectorAll(".wsrow")).toHaveLength(1);
});

it("keeps refreshing in the project menu without toggling the project", async () => {
  const { readTree, reload } = draw();
  await userEvent.click(screen.getByRole("button", { name: "项目操作：project · gpu" }));
  await userEvent.click(within(screen.getByRole("menu")).getByRole("menuitem", { name: "刷新 gpu 上的会话" }));
  expect(readTree).toHaveBeenCalledWith("gpu");
  expect(reload).toHaveBeenCalledOnce();
  expect(screen.getByText("GPU task")).toBeTruthy();
});

it("applies the conversation scope to remote projects too", () => {
  draw("archived");
  expect(screen.queryByText("GPU task")).toBeNull();
  expect(screen.getByText("Build task")).toBeTruthy();
});

it("leaves remote idle sessions out of the running filter", () => {
  draw("live");
  expect(document.querySelectorAll(".wsrow")).toHaveLength(0);
});

it("keeps a running remote pane visible before its transcript enters the history book", () => {
  draw("live", [{ id: "fresh", base: "/r/fresh", host: "gpu", root: "/work/new", name: "New remote task" }]);
  expect(screen.getByRole("treeitem", { name: "New remote task" })).toBeTruthy();
});

it("uses the far kernel's pin state in the unified pinned filter", () => {
  draw("pinned", [], { gpu: [{ root: "/work/project", name: "project", sessions: [
    { path: "/s/pin", name: "Pinned remote", pinned: true },
    { path: "/s/plain", name: "Plain remote" },
  ] }], build: [] });
  expect(screen.getByText("Pinned remote")).toBeTruthy();
  expect(screen.queryByText("Plain remote")).toBeNull();
});

it("does not resurrect an archived open pane as a missing history row", () => {
  draw("all", [{ id: "archived", base: "/r/old", host: "build", root: "/work/project", name: "Archived pane", sessionPath: "/s/a" }]);
  expect(screen.queryByText("Build task")).toBeNull();
  expect(screen.queryByText("Archived pane")).toBeNull();
});

it("counts older remote history before applying the archive filter", () => {
  draw("all", [], { gpu: [{ root: "/work/project", name: "project", total: 2, sessions: [
    { path: "/s/one", name: "One" }, { path: "/s/old", name: "Archived", archived: true },
  ] }], build: [] });
  expect(screen.queryByText(/较早的会话/)).toBeNull();
});
