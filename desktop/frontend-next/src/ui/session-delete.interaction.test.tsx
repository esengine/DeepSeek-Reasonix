// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Workspaces } from "./Workspaces";
import { MockHub } from "../port/mock_hub";
import type { HubPort, RuntimeView, TreeWorkspace } from "../port/hub";

afterEach(cleanup);

const SESSION = "/w/.reasonix/sessions/20260901-120000.jsonl";

function tree(over: Partial<TreeWorkspace> = {}): TreeWorkspace[] {
  return [
    {
      root: "/w",
      name: "w",
      sessions: [{ path: SESSION, name: "20260901-120000", title: "the one to delete" }],
      ...over,
    } as TreeWorkspace,
  ];
}

function draw(over: { runtimes?: RuntimeView[]; workspaces?: TreeWorkspace[] } = {}) {
  const hub = new MockHub() as unknown as HubPort;
  const removeSession = vi.spyOn(hub, "removeSession").mockResolvedValue(undefined);
  const onClose = vi.fn().mockResolvedValue(undefined);
  const onOpen = vi.fn().mockResolvedValue(undefined);
  const reload = vi.fn().mockResolvedValue(undefined);
  const onError = vi.fn();
  const view = (workspaces: TreeWorkspace[]) => (
    <Workspaces
      hub={hub}
      tree={workspaces}
      treeRead
      runtimes={over.runtimes ?? []}
      active=""
      folded={new Set()}
      onFold={() => {}}
      reload={reload}
      onOpen={onOpen}
      onFocus={() => {}}
      onClose={onClose}
      liveIds={() => []}
      runs={{}}
      onRename={() => {}}
      onError={onError}
      adder={{ add: () => {}, close: () => {}, at: null } as never}
    />
  );
  const { rerender } = render(view(over.workspaces ?? tree()));
  // What the kernel lists after a write: the tree the next reload brings back.
  const relist = (workspaces: TreeWorkspace[]) => rerender(view(workspaces));
  return { removeSession, onClose, onOpen, reload, onError, relist };
}

it("projects each open session's run state onto its own row", () => {
  const runtime = { id: "r1", base: "", root: "/w", name: "w", sessionPath: SESSION };
  const workspaces = tree({ sessions: [{ path: SESSION, name: "session", title: "running session", runtimeId: runtime.id }] });
  const onClose = vi.fn(async () => {});
  const onError = vi.fn();
  render(
    <Workspaces
      hub={{} as never}
      tree={workspaces}
      treeRead
      runtimes={[runtime]}
      active=""
      folded={new Set()}
      onFold={() => {}}
      reload={async () => {}}
      onOpen={async () => {}}
      onFocus={() => {}}
      onClose={onClose}
      liveIds={() => [runtime.id]}
      runs={{ [runtime.id]: { run: "running", live: true } }}
      onRename={() => {}}
      onError={onError}
      adder={{ add: () => {}, close: () => {}, at: null } as never}
    />,
  );

  expect(screen.getByRole("treeitem", { name: /running session/ }).getAttribute("data-run")).toBe("running");
});

const trash = async () => {
  await userEvent.pointer({ keys: "[MouseRight]", target: screen.getByRole("treeitem", { name: /the one to delete|open here/ }) });
  return screen.getByRole("menuitem", { name: "删除会话" });
};
const confirmGo = () => within(screen.getByRole("alertdialog")).getByRole("button", { name: "删除" });

describe("deleting a conversation from the rail", () => {
  // The whole gesture end to end: the row, the trash, and the confirmation that
  // replaces the row.
  it("asks, then deletes what it asked about", async () => {
    const { removeSession, reload } = draw();
    await userEvent.click(await trash());
    expect(screen.getByRole("alertdialog")).toBeTruthy();

    await userEvent.click(confirmGo());
    expect(removeSession).toHaveBeenCalledWith(SESSION);
    expect(reload).toHaveBeenCalled();
  });

  it("deletes nothing when the question is dismissed", async () => {
    const { removeSession } = draw();
    await userEvent.click(await trash());
    await userEvent.click(within(screen.getByRole("alertdialog")).getByRole("button", { name: "取消" }));
    expect(screen.queryByRole("alertdialog")).toBeNull();
    expect(removeSession).not.toHaveBeenCalled();
  });

  // The kernel refuses a conversation a pane still holds, so the delete must be
  // sequenced after the close rather than issued alongside it.
  it("closes the pane first and waits for it", async () => {
    const order: string[] = [];
    const { removeSession, onClose } = draw({
      workspaces: [
        {
          root: "/w",
          name: "w",
          sessions: [{ path: SESSION, name: "20260901-120000", title: "open here", runtimeId: "r1" }],
        } as TreeWorkspace,
      ],
    });
    onClose.mockImplementation(async () => void order.push("close"));
    removeSession.mockImplementation(async () => void order.push("remove"));

    await userEvent.click(await trash());
    await userEvent.click(confirmGo());
    expect(order).toEqual(["close", "remove"]);
    expect(onClose).toHaveBeenCalledWith(["r1"]);
  });
});

describe("the conversation action menu", () => {
  it("opens on a title right-click without opening the conversation; left-click still opens it", async () => {
    const { onOpen } = draw();
    await userEvent.pointer({ keys: "[MouseRight]", target: screen.getByText("the one to delete") });
    expect(screen.getByRole("menu", { name: "会话操作" })).toBeTruthy();
    expect(onOpen).not.toHaveBeenCalled();
    await userEvent.keyboard("{Escape}");
    await userEvent.click(screen.getByText("the one to delete"));
    expect(onOpen).toHaveBeenCalledWith({ root: "/w", sessionPath: SESSION });
  });

  it("keeps repeated right-clicks open and returns focus on Escape", async () => {
    draw();
    const row = screen.getByRole("treeitem", { name: /the one to delete/ });
    await userEvent.pointer({ keys: "[MouseRight]", target: row });
    await userEvent.pointer({ keys: "[MouseRight]", target: row });
    expect(screen.getByRole("menu", { name: "会话操作" })).toBeTruthy();
    await userEvent.keyboard("{Escape}");
    expect(screen.queryByRole("menu", { name: "会话操作" })).toBeNull();
    expect(document.activeElement).toBe(row);
  });

  it("closes when the reader clicks outside it", async () => {
    draw();
    await userEvent.pointer({ keys: "[MouseRight]", target: screen.getByRole("treeitem", { name: /the one to delete/ }) });
    expect(screen.getByRole("menu", { name: "会话操作" })).toBeTruthy();

    await userEvent.click(document.body);
    expect(screen.queryByRole("menu", { name: "会话操作" })).toBeNull();
  });
});

describe("the Delete key on a focused conversation", () => {
  const row = () => screen.getByRole("treeitem", { name: /the one to delete/ });

  it("asks through the same confirmation the menu uses, and Escape deletes nothing", async () => {
    const { removeSession } = draw();
    row().focus();
    await userEvent.keyboard("{Delete}");
    expect(screen.getByRole("alertdialog", { name: "删除「the one to delete」？" })).toBeTruthy();

    await userEvent.keyboard("{Escape}");
    expect(screen.queryByRole("alertdialog")).toBeNull();
    expect(removeSession).not.toHaveBeenCalled();
    expect(document.activeElement).toBe(row());
  });

  it("deletes once the question is answered", async () => {
    const { removeSession } = draw();
    row().focus();
    await userEvent.keyboard("{Delete}");
    await userEvent.click(confirmGo());
    expect(removeSession).toHaveBeenCalledWith(SESSION);
  });

  it("acts on the focused row only, never the hovered one", async () => {
    draw();
    await userEvent.hover(row());
    await userEvent.keyboard("{Delete}");
    expect(screen.queryByRole("alertdialog")).toBeNull();
  });

  it("does not pull focus back to a row whose question another one replaced", async () => {
    const OTHER = "/w/.reasonix/sessions/20260902-120000.jsonl";
    draw({
      workspaces: tree({
        sessions: [
          { path: SESSION, name: "20260901-120000", title: "the one to delete" },
          { path: OTHER, name: "20260902-120000", title: "the other one" },
        ],
      }),
    });
    row().focus();
    await userEvent.keyboard("{Delete}");
    await userEvent.pointer({ keys: "[MouseRight]", target: screen.getByRole("treeitem", { name: /the other one/ }) });
    await userEvent.click(screen.getByRole("menuitem", { name: "删除会话" }));

    const other = screen.getByRole("alertdialog", { name: "删除「the other one」？" });
    expect(other.contains(document.activeElement)).toBe(true);
  });

  it("hands focus to the next row once the focused one is deleted", async () => {
    const NEXT = "/w/.reasonix/sessions/20260902-120000.jsonl";
    const both = tree({
      sessions: [
        { path: SESSION, name: "20260901-120000", title: "the one to delete" },
        { path: NEXT, name: "20260902-120000", title: "the next one" },
      ],
    });
    const { reload, relist } = draw({ workspaces: both });
    reload.mockImplementation(async () => relist(tree({ sessions: [both[0].sessions[1]] })));
    row().focus();
    await userEvent.keyboard("{Delete}");
    await userEvent.keyboard("{Enter}");

    expect(screen.queryByRole("treeitem", { name: /the one to delete/ })).toBeNull();
    expect(document.activeElement).toBe(screen.getByRole("treeitem", { name: /the next one/ }));
  });

  it("leaves a rename field's Delete to the field", async () => {
    draw();
    await userEvent.pointer({ keys: "[MouseRight]", target: screen.getByRole("treeitem", { name: /the one to delete/ }) });
    await userEvent.click(screen.getByRole("menuitem", { name: /重命名/ }));
    const field = screen.getByRole("textbox", { name: "重命名该会话" });
    expect(fireEvent.contextMenu(field)).toBe(true);
    field.focus();
    await userEvent.keyboard("{Delete}");
    expect(screen.queryByRole("alertdialog")).toBeNull();
  });
});
