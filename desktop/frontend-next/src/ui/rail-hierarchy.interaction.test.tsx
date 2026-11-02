// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { MockHub } from "../port/mock_hub";
import type { RuntimeView, TreeWorkspace } from "../port/hub";
import { Sidebar } from "./Sidebar";
import { NO_PAGING } from "./treewindow";

afterEach(cleanup);

const tree: TreeWorkspace[] = [{
  root: "/work/alpha", name: "alpha", remembered: true,
  sessions: [
    { path: "/work/alpha/a.jsonl", name: "a", title: "first" },
    { path: "/work/alpha/b.jsonl", name: "b", title: "second" },
  ],
}];

function draw(withRemote = false, runtimes: RuntimeView[] = []) {
  const hub = new MockHub();
  const noop = vi.fn();
  render(
    <Sidebar
      onFiles={noop} onView={noop} hub={hub} collapsed={false} navShown paging={NO_PAGING} tree={tree} treeRead runtimes={runtimes} runs={{}} active=""
      activeWorkspace={tree[0]} liveIds={() => []} pinned={new Set()} onPin={noop}
      remotes={withRemote ? [{ name: "gpu", target: "gpu", status: "connected", workspaces: ["/srv/work"] }] : null}
      remoteTrees={withRemote ? { gpu: [{ root: "/srv/work", name: "Remote project", sessions: [{ path: "/r/s", name: "Remote task" }] }] } : {}}
      reloadRemotes={async () => {}} reloadRemoteTrees={async () => {}}
      readRemoteTree={async () => {}} reloadTree={async () => {}} adder={{ busy: false, add: noop, addPath: noop } as never}
      onOpen={async () => {}} onOpenRemote={async () => {}} onFocusPane={noop} onClosePanes={async () => {}}
      onPause={noop} onArchive={async () => {}} onRename={noop} onCollapse={noop} account={null} accountUnread="" wallet=""
      onSettings={noop} onFeedback={noop} feedbackUnread={0} onFind={noop} onError={noop}
    />,
  );
}

const scope = (value: string) => document.querySelector(`[data-action="session.filter"][data-value="${value}"]`)!;

async function foldAll(label: string) {
  await userEvent.click(screen.getByRole("button", { name: "项目列表的更多操作" }));
  await userEvent.click(screen.getByRole("menuitem", { name: label }));
}

it("keeps scope filters quiet without showing local-only totals for a unified list", () => {
  draw();
  for (const value of ["all", "live", "pinned", "archived"]) expect(scope(value).querySelector("b")).toBeNull();
});

it("starts directly with projects instead of a local machine parent", () => {
  draw();
  expect(screen.queryByRole("treeitem", { name: /这台机器/ })).toBeNull();
  expect(screen.getByRole("button", { name: "打开项目" })).toBeTruthy();
  const project = document.querySelector<HTMLElement>(".wsrow")!;
  expect(project.getAttribute("aria-level")).toBe("1");
  expect(project.querySelector(".studio-icon")).toBeTruthy();
  const count = project.querySelector(".rowcount")!;
  expect(count.textContent).toBe("2");
  expect(count.getAttribute("data-pill")).not.toBeNull();
  expect(document.querySelector('.sessrow')?.getAttribute("aria-level")).toBe("2");
});

it("collapses and expands local and SSH projects together from the projects menu", async () => {
  draw(true);
  expect(screen.getByText("first")).toBeTruthy();
  expect(screen.getByText("Remote task")).toBeTruthy();
  await foldAll("收起所有项目");
  expect(screen.queryByText("first")).toBeNull();
  expect(screen.queryByText("Remote task")).toBeNull();
  await foldAll("展开所有项目");
  expect(screen.getByText("first")).toBeTruthy();
  expect(screen.getByText("Remote task")).toBeTruthy();
});

it("reveals search results after collapsing everything and disables a misleading fold action", async () => {
  draw(true);
  await foldAll("收起所有项目");
  await userEvent.type(screen.getByRole("searchbox", { name: "搜索会话 / 项目" }), "Remote task");
  expect(screen.getByText("Remote task")).toBeTruthy();
  await userEvent.click(screen.getByRole("button", { name: "项目列表的更多操作" }));
  expect((screen.getByRole("menuitem", { name: "收起所有项目" }) as HTMLButtonElement).disabled).toBe(true);
});

it("includes remote runtime-only projects in collapse all", async () => {
  draw(true, [{ id: "new", base: "/r/new", host: "gpu", root: "/srv/new", name: "Fresh task" }]);
  expect(screen.getByRole("treeitem", { name: "Fresh task" })).toBeTruthy();
  await foldAll("收起所有项目");
  expect(screen.queryByRole("treeitem", { name: "Fresh task" })).toBeNull();
});

it("reports no matches when neither local nor SSH projects match the search", async () => {
  draw(true);
  await userEvent.type(screen.getByRole("searchbox", { name: "搜索会话 / 项目" }), "not-a-project");
  expect(screen.getByText("没有匹配的会话")).toBeTruthy();
});

it("reports an empty unified pinned filter", async () => {
  draw(true);
  await userEvent.click(scope("pinned"));
  expect(screen.getByText("没有匹配的会话")).toBeTruthy();
});
