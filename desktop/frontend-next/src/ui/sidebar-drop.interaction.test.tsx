// @vitest-environment jsdom
import { useState } from "react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { MockHub } from "../port/mock_hub";
import type { TreeWorkspace } from "../port/hub";
import { host } from "../port/host";
import { useAddWorkspace } from "./addws";
import { install as installDrops } from "./filedrop";
import { Sidebar } from "./Sidebar";

let clock = Date.now();
beforeEach(() => {
  installDrops();
  vi.spyOn(Date, "now").mockImplementation(() => clock);
});
afterEach(() => { cleanup(); vi.restoreAllMocks(); });

const folder: TreeWorkspace = { root: "/work/project", name: "project", remembered: true, sessions: [] };

function draw(collapsed = false) {
  const hub = new MockHub();
  const add = vi.spyOn(hub, "addWorkspace").mockResolvedValue(folder);
  const pick = vi.spyOn(hub, "pickFolder");
  const remote = vi.fn(async (_host: string) => {});
  const open = vi.fn(async (_req: { root?: string; sessionPath?: string }) => {});
  const reload = vi.fn(async () => {});
  function Harness() {
    const [error, setError] = useState("");
    const adder = useAddWorkspace(hub, reload, (e) => setError(String(e)));
    return <>
      {error && <div role="alert">{error}</div>}
      <Sidebar
        hub={hub} collapsed={collapsed} tree={[]} treeRead runtimes={[]} runs={{}} active="remote-pane"
        activeWorkspace={undefined} liveIds={() => []} pinned={new Set()} onPin={() => {}}
        remotes={null} remoteTrees={{}} reloadRemotes={async () => {}} reloadRemoteTrees={async () => {}}
        readRemoteTree={async () => {}} reloadTree={reload} adder={adder} onOpen={open} onOpenRemote={remote}
        onFocusPane={() => {}} onClosePanes={async () => {}} onPause={() => {}} onArchive={async () => {}}
        onRename={() => {}} onCollapse={() => {}} account={null} accountUnread="" wallet=""
        onSettings={() => {}} onFeedback={() => {}} feedbackUnread={0} onFind={() => {}}
        onError={(e) => setError(String(e))}
      />
    </>;
  }
  render(<Harness />);
  return { hub, add, pick, remote, open, reload };
}

function drop(paths: string[], files = [new File([], "project")]) {
  clock += 1000;
  vi.spyOn(host(), "pathsForFiles").mockReturnValue(paths);
  fireEvent.drop(document.querySelector(".rail")!, {
    dataTransfer: { types: ["Files"], files, getData: () => "" },
  });
}

it("adds the dropped folder and opens the kernel's canonical root after refreshing the tree", async () => {
  const { add, pick, remote, open, reload } = draw();
  drop(["/work/alias"]);
  await waitFor(() => expect(open).toHaveBeenCalledExactlyOnceWith({ root: folder.root }));
  expect(add).toHaveBeenCalledExactlyOnceWith("/work/alias");
  expect(reload).toHaveBeenCalledOnce();
  expect(add.mock.invocationCallOrder[0]).toBeLessThan(reload.mock.invocationCallOrder[0]);
  expect(reload.mock.invocationCallOrder[0]).toBeLessThan(open.mock.invocationCallOrder[0]);
  expect(pick).not.toHaveBeenCalled();
  expect(remote).not.toHaveBeenCalled();
});

it("shows the kernel's refusal for a file or an unavailable folder and never opens it", async () => {
  const { add, open, reload } = draw();
  add.mockRejectedValueOnce(new Error("not a directory"));
  drop(["/work/readme.md"]);
  expect((await screen.findByRole("alert")).textContent).toContain("not a directory");
  expect(open).not.toHaveBeenCalled();
  expect(reload).not.toHaveBeenCalled();
});

it("explains a browser drop with no host path instead of treating file bytes as a project", async () => {
  const { add, open } = draw();
  drop([]);
  expect((await screen.findByRole("alert")).textContent).toContain("本机路径");
  expect(add).not.toHaveBeenCalled();
  expect(open).not.toHaveBeenCalled();
});

it("refuses multiple paths without adding only part of the drop", async () => {
  const { add } = draw();
  drop(["/work/a", "/work/b"]);
  expect((await screen.findByRole("alert")).textContent).toContain("一个文件夹");
  expect(add).not.toHaveBeenCalled();
});

it("ignores dropped text and links without reporting a missing folder path", () => {
  const { add, open } = draw();
  clock += 1000;
  fireEvent.drop(document.querySelector(".rail")!, {
    dataTransfer: { types: ["text/plain"], files: [], getData: () => "https://example.com" },
  });
  expect(screen.queryByRole("alert")).toBeNull();
  expect(add).not.toHaveBeenCalled();
  expect(open).not.toHaveBeenCalled();
});

it("ignores a drop while the sidebar is collapsed", async () => {
  const { add } = draw(true);
  drop([folder.root]);
  await act(async () => {});
  expect(add).not.toHaveBeenCalled();
});

it("keeps the add action busy until selection finishes and does not admit another drop", async () => {
  const { add, open } = draw();
  let finish!: () => void;
  open.mockImplementationOnce(() => new Promise<void>((resolve) => { finish = resolve; }));
  drop([folder.root]);
  await waitFor(() => expect(open).toHaveBeenCalledOnce());
  const picker = screen.getByRole("button", { name: "打开或新建项目…" });
  expect(picker.hasAttribute("data-busy")).toBe(true);
  drop(["/work/other"]);
  expect(add).toHaveBeenCalledOnce();
  await act(async () => finish());
  await waitFor(() => expect(picker.hasAttribute("data-busy")).toBe(false));
});

it("reports a failed open after adding the folder, then allows the next drop", async () => {
  const { add, open } = draw();
  open.mockRejectedValueOnce(new Error("could not open session"));
  drop([folder.root]);
  expect((await screen.findByRole("alert")).textContent).toContain("could not open session");
  drop([folder.root]);
  await waitFor(() => expect(open).toHaveBeenCalledTimes(2));
  expect(add).toHaveBeenCalledTimes(2);
});

it("waits for the refreshed tree before selecting and reports a failed refresh", async () => {
  const { open, reload } = draw();
  let fail!: (error: Error) => void;
  reload.mockImplementationOnce(() => new Promise<void>((_, reject) => { fail = reject; }));
  drop([folder.root]);
  await waitFor(() => expect(reload).toHaveBeenCalledOnce());
  expect(open).not.toHaveBeenCalled();
  await act(async () => fail(new Error("tree unavailable")));
  expect((await screen.findByRole("alert")).textContent).toContain("tree unavailable");
  expect(open).not.toHaveBeenCalled();
});

it("shows a folder-drop hint only while hovering a visible, idle sidebar", async () => {
  draw();
  const rail = document.querySelector(".rail")!;
  fireEvent.dragOver(rail, { dataTransfer: { types: ["Files"] } });
  expect(screen.getByText("松开以添加项目文件夹")).toBeTruthy();
  fireEvent.dragLeave(rail, { dataTransfer: { types: ["Files"] }, relatedTarget: null });
  expect(screen.queryByText("松开以添加项目文件夹")).toBeNull();
});

it("preserves the existing Add folder picker path", async () => {
  const { add, pick, open } = draw();
  pick.mockResolvedValueOnce("/work/picked");
  await userEvent.click(screen.getByRole("button", { name: "打开或新建项目…" }));
  await waitFor(() => expect(add).toHaveBeenCalledWith("/work/picked"));
  expect(open).not.toHaveBeenCalled();
});
