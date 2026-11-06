// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Sidebar } from "./Sidebar";
import { MockHub } from "../port/mock_hub";
import { HttpError } from "../port/http_error";
import { NO_PAGING } from "./treewindow";

beforeEach(() => localStorage.clear());
afterEach(() => { cleanup(); vi.restoreAllMocks(); localStorage.clear(); });

function draw(onOpen: (req: { root?: string; sessionPath?: string }) => Promise<unknown>, hub = new MockHub(), onError = vi.fn()) {
  const noop = vi.fn();
  render(<Sidebar hub={hub} collapsed={false} navShown treeRead
    tree={[{ root: "/gone/alpha", name: "alpha", remembered: true,
      sessions: [{ path: "/gone/alpha/a.jsonl", name: "a", title: "first" }] }]}
    runtimes={[]} runs={{}} active="" activeWorkspace={undefined} liveIds={() => []}
    pinned={new Set()} onPin={noop} remotes={null} remoteTrees={{}}
    reloadRemotes={async () => {}} reloadRemoteTrees={async () => {}} readRemoteTree={async () => {}}
    reloadTree={async () => {}} paging={NO_PAGING} adder={{ busy: false, add: noop, addPath: noop } as never}
    onOpen={onOpen as never} onOpenRemote={async () => {}} onFocusPane={noop} onClosePanes={async () => {}}
    onPause={noop} onArchive={async () => {}} onRename={noop} onCollapse={noop}
    wallet="" onSettings={noop} onFiles={noop} onView={noop}
    onFeedback={noop} feedbackUnread={0} onFind={noop} onError={onError} />);
  return { hub, onError };
}

const missing = () => new HttpError(400, "stat /gone/alpha: no such file or directory", { code: "workspace.folder_missing" });

it("asks, in Chinese, whether to remove a project whose folder is gone, instead of printing the system's sentence", async () => {
  const { onError } = draw(async () => { throw missing(); });
  await userEvent.click(screen.getByText("first"));
  expect(await screen.findByText("「alpha」的文件夹已经不在磁盘上了，要把它从列表里移除吗？")).toBeTruthy();
  expect(onError).not.toHaveBeenCalled();
});

it("removes the project only when told to", async () => {
  const hub = new MockHub();
  const remove = vi.spyOn(hub, "removeWorkspace");
  draw(async () => { throw missing(); }, hub);
  await userEvent.click(screen.getByText("first"));
  await userEvent.click(await screen.findByRole("button", { name: "取消" }));
  expect(remove).not.toHaveBeenCalled();
  await userEvent.click(screen.getByText("first"));
  await userEvent.click(await screen.findByRole("button", { name: "移除" }));
  await waitFor(() => expect(remove).toHaveBeenCalledWith("/gone/alpha"));
});

it("keeps reporting every other failure as an error", async () => {
  const { onError } = draw(async () => { throw new HttpError(500, "boom", { code: "session.open_failed" }); });
  await userEvent.click(screen.getByText("first"));
  await waitFor(() => expect(onError).toHaveBeenCalled());
  expect(screen.queryByText(/文件夹已经不在磁盘上/)).toBeNull();
});

it("draws the new-session button of a project row as a plus, as the Sess-Rows board does", () => {
  draw(async () => {});
  const icon = document.querySelector(".wsrow .wsadd svg path");
  expect(icon?.getAttribute("d")).toBe("M12 5v14M5 12h14");
});

it("draws a remote project's new-session button as a plus too", () => {
  const noop = vi.fn();
  render(<Sidebar hub={new MockHub()} collapsed={false} navShown treeRead tree={[]} runtimes={[]} runs={{}} active="" activeWorkspace={undefined}
    liveIds={() => []} pinned={new Set()} onPin={noop}
    remotes={[{ name: "server", target: "server", status: "connected", workspace: "/work/alpha" }]}
    remoteTrees={{ server: [{ root: "/work/alpha", name: "remote-alpha", remembered: true, sessions: [{ path: "/work/alpha/r.jsonl", name: "r", title: "remote session" }] }] }}
    reloadRemotes={async () => {}} reloadRemoteTrees={async () => {}} readRemoteTree={async () => {}} reloadTree={async () => {}} paging={NO_PAGING}
    adder={{ busy: false, add: noop, addPath: noop } as never} onOpen={async () => {}} onOpenRemote={async () => {}} onFocusPane={noop}
    onClosePanes={async () => {}} onPause={noop} onArchive={async () => {}} onRename={noop} onCollapse={noop} wallet="" onSettings={noop}
    onFiles={noop} onView={noop} onFeedback={noop} feedbackUnread={0} onFind={noop} onError={noop} />);
  const paths = [...document.querySelectorAll(".wsadd svg path")].map((p) => p.getAttribute("d"));
  expect(paths.length).toBeGreaterThan(0);
  expect(paths.every((d) => d === "M12 5v14M5 12h14")).toBe(true);
});
