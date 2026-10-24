// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { MockHub } from "../port/mock_hub";
import type { TreeWorkspace } from "../port/hub";
import { Sidebar } from "./Sidebar";
import { NO_PAGING } from "./treewindow";

afterEach(cleanup);

const tree: TreeWorkspace[] = [{ root: "/work/alpha", name: "alpha", remembered: true, sessions: [{ path: "/work/alpha/a.jsonl", name: "a", title: "first" }] }];

function draw(navShown: boolean, onSettings = vi.fn()) {
  const noop = vi.fn();
  render(
    <Sidebar
      onFiles={noop} onView={noop} hub={new MockHub()} collapsed={false} navShown={navShown} paging={NO_PAGING} tree={tree} treeRead runtimes={[]} runs={{}} active=""
      activeWorkspace={tree[0]} liveIds={() => []} pinned={new Set()} onPin={noop} remotes={null} remoteTrees={{}}
      reloadRemotes={async () => {}} reloadRemoteTrees={async () => {}} readRemoteTree={async () => {}} reloadTree={async () => {}}
      adder={{ busy: false, add: noop, addPath: noop } as never} onOpen={async () => {}} onOpenRemote={async () => {}} onFocusPane={noop} onClosePanes={async () => {}}
      onPause={noop} onArchive={async () => {}} onRename={noop} onCollapse={noop} account={null} accountUnread="" wallet=""
      onSettings={onSettings} onFeedback={noop} feedbackUnread={0} onFind={noop} onError={noop}
    />,
  );
}

const remoteEntries = () => document.querySelectorAll('[data-action="settings.section"][data-value="remote"]');

it("has no remote entry of its own while the icon rail is shown", () => {
  draw(true);
  expect(remoteEntries()).toHaveLength(0);
});

it("without the icon rail has one way into the remote settings, and it opens that section", async () => {
  const onSettings = vi.fn();
  draw(false, onSettings);
  expect(remoteEntries()).toHaveLength(1);
  expect(screen.queryByRole("button", { name: /连接与机器/ })).toBeNull();
  await userEvent.click(remoteEntries()[0] as HTMLElement);
  expect(onSettings).toHaveBeenCalledWith("remote");
});

it("draws the shortcut rows with their label only, the shortcuts staying on the tooltip", () => {
  draw(false);
  expect(document.querySelectorAll(".studio-quicknav small, .studio-new-task kbd, .studio-search kbd")).toHaveLength(0);
  expect(document.querySelector(".studio-new-task")?.getAttribute("title")).toBe("Alt N");
  expect(document.querySelector(".studio-search")?.getAttribute("title")).toBe("Ctrl K");
});
