// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Workspaces } from "./Workspaces";
import { RailSearch } from "./railsearch";
import type { HubPort, TreeSession, TreeWorkspace } from "../port/hub";
import type { TreePaging } from "./treewindow";

afterEach(cleanup);

const rows = (n: number, archived: number[] = []): TreeSession[] =>
  Array.from({ length: n }, (_, i) => ({
    path: `/w/.reasonix/sessions/s${i}.jsonl`,
    name: `s${i}`,
    title: `chat ${i}`,
    turns: 1,
    archived: archived.includes(i) || undefined,
  }));

// What the kernel sends for the default (active) view: `remaining` counts the
// active rows past the window, and `total`/`archived` cover the whole folder.
const workspace = (loaded: number, total: number, archived = 0, archivedAt: number[] = []): TreeWorkspace[] => [
  {
    root: "/w", name: "w", total, archived: archived || undefined,
    remaining: Math.max(0, total - archived - (loaded - archivedAt.length)),
    sessions: rows(loaded, archivedAt),
  },
];

function draw(tree: TreeWorkspace[], paging: Partial<TreePaging> = {}) {
  const pager: TreePaging = { more: vi.fn().mockResolvedValue(undefined), view: vi.fn(), ...paging };
  const view = (workspaces: TreeWorkspace[]) => (
    <RailSearch>
      <Workspaces
        hub={{} as HubPort}
        tree={workspaces}
        treeRead
        runtimes={[]}
        active=""
        folded={new Set()}
        onFold={() => {}}
        reload={async () => {}}
        paging={pager}
        onOpen={async () => {}}
        onFocus={() => {}}
        onClose={async () => {}}
        liveIds={() => []}
        runs={{}}
        onRename={() => {}}
        onError={() => {}}
        adder={{ add: () => {}, close: () => {}, at: null } as never}
      />
    </RailSearch>
  );
  const { rerender } = render(view(tree));
  return { pager, relist: (workspaces: TreeWorkspace[]) => rerender(view(workspaces)) };
}

describe("a workspace with more conversations than the kernel sent", () => {
  it("says how many are left to load beneath every loaded row, with no count on the project", () => {
    draw(workspace(50, 132));
    expect(screen.getByRole("button", { name: "显示更多（还有 82 个）" })).toBeTruthy();
    expect(document.querySelector(".rowcount")).toBeNull();
    expect(screen.getAllByText(/^chat \d+$/)).toHaveLength(50);
  });

  it("asks for the next page and shows it when the kernel answers", async () => {
    const { pager, relist } = draw(workspace(50, 132));
    await userEvent.click(screen.getByRole("button", { name: "显示更多（还有 82 个）" }));
    expect(pager.more).toHaveBeenCalledWith("/w");
    relist(workspace(100, 132));
    expect(screen.getAllByText(/^chat \d+$/)).toHaveLength(100);
    expect(screen.getByRole("button", { name: "显示更多（还有 32 个）" })).toBeTruthy();
  });

  it("disables the button while a page is loading", async () => {
    let done!: () => void;
    const more = vi.fn(() => new Promise<void>((r) => (done = r)));
    draw(workspace(50, 132), { more });
    await userEvent.click(screen.getByRole("button", { name: "显示更多（还有 82 个）" }));
    const busy = screen.getByRole("button", { name: "正在加载…" }) as HTMLButtonElement;
    expect(busy.disabled).toBe(true);
    done();
    await waitFor(() => expect(screen.queryByRole("button", { name: "正在加载…" })).toBeNull());
    expect(more).toHaveBeenCalledTimes(1);
  });

  it("says the page failed and retries on the next click", async () => {
    const more = vi.fn().mockRejectedValueOnce(new Error("down")).mockResolvedValue(undefined);
    draw(workspace(50, 132), { more });
    await userEvent.click(screen.getByRole("button", { name: "显示更多（还有 82 个）" }));
    await userEvent.click(await screen.findByRole("button", { name: "加载失败 · 重试" }));
    expect(more).toHaveBeenCalledTimes(2);
    expect(screen.queryByRole("button", { name: "加载失败 · 重试" })).toBeNull();
  });

  it("offers nothing more once everything is loaded", () => {
    draw(workspace(132, 132));
    expect(screen.getAllByText(/^chat \d+$/)).toHaveLength(132);
    expect(screen.queryByRole("button", { name: /显示更多/ })).toBeNull();
  });

  it("treats a kernel that sends no total as having sent everything", () => {
    draw([{ root: "/w", name: "w", sessions: rows(7) }]);
    expect(screen.getAllByText(/^chat \d+$/)).toHaveLength(7);
    expect(screen.queryByRole("button", { name: /显示更多/ })).toBeNull();
  });

  it("leaves archived conversations out of what is left to load", () => {
    draw(workspace(50, 132, 2, [3]));
    expect(screen.getByRole("button", { name: "显示更多（还有 81 个）" })).toBeTruthy();
  });

  it("follows a delete: what is left and the rows fall together", () => {
    const { relist } = draw(workspace(50, 132));
    relist(workspace(50, 131));
    expect(screen.getByRole("button", { name: "显示更多（还有 81 个）" })).toBeTruthy();
    relist(workspace(49, 49));
    expect(screen.getAllByText(/^chat \d+$/)).toHaveLength(49);
    expect(screen.queryByRole("button", { name: /显示更多/ })).toBeNull();
  });
});

describe("what the window tells the kernel it is looking at", () => {
  const lastView = (pager: TreePaging) => (pager.view as ReturnType<typeof vi.fn>).mock.lastCall?.[0];

  it("asks for the active rows by default", () => {
    const { pager } = draw(workspace(50, 132));
    expect(lastView(pager)).toEqual({ scope: "active", q: "" });
  });

  it("asks for every row, filtered by the word, once a search settles, and hides the page button only for the cap", async () => {
    const { pager } = draw(workspace(50, 132));
    await userEvent.type(screen.getByRole("searchbox", { name: "搜索会话 / 项目" }), "chat 7");
    await waitFor(() => expect(lastView(pager)).toEqual({ scope: "", q: "chat 7" }));
    await userEvent.clear(screen.getByRole("searchbox", { name: "搜索会话 / 项目" }));
    await waitFor(() => expect(lastView(pager)).toEqual({ scope: "active", q: "" }));
  });

  it("does not ask the kernel for a word until typing pauses", async () => {
    const { pager } = draw(workspace(50, 132));
    const box = screen.getByRole("searchbox", { name: "搜索会话 / 项目" });
    await userEvent.type(box, "chat 7");
    const asked = (pager.view as ReturnType<typeof vi.fn>).mock.calls.map((c) => c[0].q);
    expect(asked.filter((q) => q !== "")).toEqual([]);
    await waitFor(() => expect(lastView(pager)?.q).toBe("chat 7"));
  });
});

describe("the most a folder will load", () => {
  it("replaces the button with a pointer to search once the cap is reached", () => {
    draw(workspace(300, 1000));
    expect(screen.queryByRole("button", { name: /显示更多/ })).toBeNull();
    expect(screen.getByText("还有 700 个较早的会话 · 用搜索查找")).toBeTruthy();
  });
});
