// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Sidebar } from "./Sidebar";
import { MockHub } from "../port/mock_hub";
import { NO_PAGING } from "./treewindow";
import { setProjectsFolded } from "../state/sidebarFolds";

beforeEach(() => localStorage.clear());
afterEach(() => { cleanup(); vi.restoreAllMocks(); setProjectsFolded(false); localStorage.clear(); });

function draw(remote = false) {
  const noop = vi.fn();
  return render(<Sidebar hub={new MockHub()} collapsed={false} navShown treeRead
    tree={[{ root: "/work/alpha", name: "alpha", remembered: true,
      sessions: [{ path: "/work/alpha/a.jsonl", name: "a", title: "first" }] }]}
    runtimes={[]} runs={{}} active="" activeWorkspace={undefined} liveIds={() => []}
    pinned={new Set()} onPin={noop}
    remotes={remote ? [{ name: "server", target: "server", status: "connected", workspace: "/work/alpha" }] : null}
    remoteTrees={remote ? { server: [{ root: "/work/alpha", name: "remote-alpha", remembered: true,
      sessions: [{ path: "/work/alpha/remote.jsonl", name: "remote", title: "remote session" }] }] } : {}}
    reloadRemotes={async () => {}} reloadRemoteTrees={async () => {}} readRemoteTree={async () => {}}
    reloadTree={async () => {}} paging={NO_PAGING} adder={{ busy: false, add: noop, addPath: noop } as never}
    onOpen={async () => {}} onOpenRemote={async () => {}} onFocusPane={noop} onClosePanes={async () => {}}
    onPause={noop} onArchive={async () => {}} onRename={noop} onCollapse={noop}
    account={null} accountUnread="" wallet="" onSettings={noop} onFiles={noop} onView={noop}
    onFeedback={noop} feedbackUnread={0} onFind={noop} onError={noop} />);
}

const project = () => screen.getByRole("treeitem", { name: /alpha/ });

it("remembers individual collapse and expansion across sidebar remounts", async () => {
  let view = draw();
  expect(project().getAttribute("aria-expanded")).toBe("true");
  await userEvent.click(project());
  view.unmount();
  view = draw();
  expect(project().getAttribute("aria-expanded")).toBe("false");
  await userEvent.click(project());
  view.unmount();
  draw();
  expect(project().getAttribute("aria-expanded")).toBe("true");
});

it("remembers collapse-all and expand-all across sidebar remounts", async () => {
  let view = draw();
  await userEvent.click(screen.getByRole("button", { name: "项目列表的更多操作" }));
  await userEvent.click(screen.getByRole("menuitem", { name: "收起所有项目" }));
  view.unmount();
  view = draw();
  expect(project().getAttribute("aria-expanded")).toBe("false");
  await userEvent.click(screen.getByRole("button", { name: "项目列表的更多操作" }));
  await userEvent.click(screen.getByRole("menuitem", { name: "展开所有项目" }));
  view.unmount();
  draw();
  expect(project().getAttribute("aria-expanded")).toBe("true");
});

it("folds the whole projects section without resetting filters or local and remote folds", async () => {
  draw(true);
  await userEvent.click(screen.getByRole("treeitem", { name: /remote-alpha/ }));
  await userEvent.click(screen.getByRole("button", { name: "进行中" }));
  await userEvent.click(within(screen.getByRole("heading", { name: "项目" })).getByText("项目"));
  expect(screen.queryByRole("group", { name: "会话范围" })).toBeNull();
  expect(screen.queryByRole("tree")).toBeNull();
  expect(screen.getByRole("button", { name: "打开项目" })).toBeTruthy();
  expect(screen.getByRole("button", { name: "项目列表的更多操作" })).toBeTruthy();
  const toggle = screen.getByRole("button", { name: "展开项目" });
  toggle.focus();
  await userEvent.keyboard("{Enter}");
  expect(screen.getByRole("button", { name: "进行中" }).getAttribute("aria-pressed")).toBe("true");
  await userEvent.click(screen.getByRole("button", { name: "全部" }));
  expect(screen.getByRole("treeitem", { name: /remote-alpha/ }).getAttribute("aria-expanded")).toBe("false");
  expect(screen.getByRole("treeitem", { name: /^alpha/ }).getAttribute("aria-expanded")).toBe("true");
});

it("remembers the section fold independently and reveals selection from its menu", async () => {
  const view = draw();
  await userEvent.click(screen.getByRole("button", { name: "收起项目" }));
  view.unmount();
  draw();
  expect(screen.queryByRole("tree")).toBeNull();
  await userEvent.click(screen.getByRole("button", { name: "项目列表的更多操作" }));
  await userEvent.click(screen.getByRole("menuitem", { name: "选择本机会话" }));
  expect(screen.getByRole("tree").getAttribute("aria-multiselectable")).toBe("true");
  expect(project().getAttribute("aria-expanded")).toBe("true");
});

it("keeps folding usable when storage cannot be written", async () => {
  draw();
  vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => { throw new DOMException("blocked"); });
  await userEvent.click(project());
  expect(project().getAttribute("aria-expanded")).toBe("false");
  await userEvent.click(project());
  expect(project().getAttribute("aria-expanded")).toBe("true");
  await userEvent.click(screen.getByRole("button", { name: "收起项目" }));
  expect(screen.queryByRole("tree")).toBeNull();
  await userEvent.click(screen.getByRole("button", { name: "展开项目" }));
  expect(screen.getByRole("tree")).toBeTruthy();
});


it("keeps a remote project's fold separate from the same local path", async () => {
  const view = draw(true);
  await userEvent.click(screen.getByRole("treeitem", { name: /remote-alpha/ }));
  view.unmount();
  draw(true);
  expect(screen.getByRole("treeitem", { name: /remote-alpha/ }).getAttribute("aria-expanded")).toBe("false");
  expect(screen.getByRole("treeitem", { name: /^alpha/ }).getAttribute("aria-expanded")).toBe("true");
});
