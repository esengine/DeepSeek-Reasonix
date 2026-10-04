// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { RailSearch } from "./railsearch";
import { RemoteHosts } from "./RemoteHosts";
import type { RemoteHost } from "../port/remote";
import type { HubPort, RuntimeView, TreeWorkspace } from "../port/hub";

vi.mock("../port/host", () => ({ host: () => ({ revealsFiles: () => true }) }));

afterEach(cleanup);

const host: RemoteHost = { name: "gpu", target: "ada@10.0.0.4", status: "connected", workspaces: ["/home/ada/training"] };
const path = "/home/ada/training/s1.jsonl";
const tree: Record<string, TreeWorkspace[] | null> = {
  gpu: [{ root: "/home/ada/training", name: "training", sessions: [{ path, name: "s1", title: "跑通构建" }] }],
};

function draw(runtimes: RuntimeView[] = [], live: string[] = []) {
  const order: string[] = [];
  const removeRemoteSession = vi.fn(async () => { order.push("remove"); });
  const readTree = vi.fn(async () => { order.push("read"); });
  const onClose = vi.fn(async () => { order.push("close"); });
  render(
    <RailSearch>
      <RemoteHosts
        hub={{ removeRemoteSession } as unknown as HubPort}
        hosts={[host]}
        runtimes={runtimes}
        active=""
        onOpen={async () => {}}
        onFocus={() => {}}
        reload={async () => {}}
        trees={tree}
        reloadTrees={async () => {}}
        readTree={readTree}
        onClose={onClose}
        liveIds={(ids) => ids.filter((id) => live.includes(id))}
        onError={() => {}}
      />
    </RailSearch>,
  );
  return { removeRemoteSession, readTree, onClose, order };
}

describe("a project on another machine", () => {
  it("offers no way to show it in this machine's file manager", () => {
    draw();
    expect(document.querySelector('[data-action="workspace.reveal"]')).toBeNull();
    expect(screen.queryByRole("menuitem", { name: /在文件管理器中显示/ })).toBeNull();
  });
});

describe("a conversation on another machine", () => {
  it("is deleted on that machine after a confirmation, and its list is read again", async () => {
    const { removeRemoteSession, order } = draw();
    await userEvent.click(screen.getByRole("button", { name: "删除会话：跑通构建" }));
    expect(removeRemoteSession).not.toHaveBeenCalled();

    await userEvent.click(screen.getByRole("button", { name: "删除" }));
    await waitFor(() => expect(order).toEqual(["remove", "read"]));
    expect(removeRemoteSession).toHaveBeenCalledWith("gpu", path);
  });

  it("closes the pane holding it before asking that machine to erase it", async () => {
    const pane = { id: "rt-1", sessionPath: path, host: "gpu" } as unknown as RuntimeView;
    const { onClose, order } = draw([pane]);
    await userEvent.click(screen.getByRole("button", { name: "删除会话：跑通构建" }));
    await userEvent.click(screen.getByRole("button", { name: "删除" }));
    await waitFor(() => expect(order).toEqual(["close", "remove", "read"]));
    expect(onClose).toHaveBeenCalledWith(["rt-1"]);
  });
});

describe("a running conversation on another machine", () => {
  it("is not closed to make way for its deletion; the far kernel answers for it", async () => {
    const pane = { id: "rt-1", sessionPath: path, host: "gpu" } as unknown as RuntimeView;
    const { onClose, removeRemoteSession } = draw([pane], ["rt-1"]);
    await userEvent.click(screen.getByRole("button", { name: "删除会话：跑通构建" }));
    expect(screen.getByText(/正在运行，停止后才能删除/)).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "删除" }));
    await waitFor(() => expect(removeRemoteSession).toHaveBeenCalled());
    expect(onClose).not.toHaveBeenCalled();
  });
});
