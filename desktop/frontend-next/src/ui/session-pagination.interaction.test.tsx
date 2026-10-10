// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { useState } from "react";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Workspaces } from "./Workspaces";
import { RailSearch } from "./railsearch";
import { boot, STORAGE } from "../i18n";
import { MockHub } from "../port/mock_hub";
import type { TreeWorkspace } from "../port/hub";

const BATCH = 10;
const MORE = /还有 \d+ 个 · 展开显示/;
const workspace = (root: string, count: number): TreeWorkspace => ({
  root,
  name: "project",
  sessions: Array.from({ length: count }, (_, i) => ({
    path: `${root}/${i}.jsonl`, name: `chat-${root}-${i}`, title: `chat-${root}-${i}`,
  })),
});

function draw(tree: TreeWorkspace[], scope: "all" | "pinned" = "all", pinned = new Set<string>(), active = "", opening = "") {
  const onOpen = vi.fn().mockResolvedValue(undefined);
  const onFocus = vi.fn();
  function Fixture({ tree, active, opening }: { tree: TreeWorkspace[]; active: string; opening: string }) {
    const [folded, setFolded] = useState(new Set<string>());
    const runtimes = tree.flatMap((ws) => ws.sessions.flatMap((session) => session.runtimeId ? [{
      id: session.runtimeId, base: `/rt/${session.runtimeId}`, root: ws.root, name: session.name, sessionPath: session.path,
    }] : []));
    const runs = Object.fromEntries(runtimes.map((rt) => [rt.id, { run: "running", live: true }]));
    return <RailSearch><Workspaces hub={new MockHub()} tree={tree} treeRead
      runtimes={runtimes} active={active} opening={opening} folded={folded} onFold={(root, closed) => setFolded((prev) => {
        const next = new Set(prev);
        if (closed) next.add(root); else next.delete(root);
        return next;
      })} reload={async () => {}} onOpen={onOpen} onFocus={onFocus} onClose={async () => {}}
      liveIds={(ids) => ids.filter((id) => runs[id]?.live)} runs={runs} scope={scope} pinned={pinned}
      onRename={() => {}} onError={() => {}} adder={{ add: () => {}, close: () => {}, at: null } as never} /></RailSearch>;
  }
  const { rerender } = render(<Fixture tree={tree} active={active} opening={opening} />);
  return { onOpen, onFocus, relist: (next: TreeWorkspace[], nextActive = active, nextOpening = opening) => rerender(<Fixture tree={next} active={nextActive} opening={nextOpening} />) };
}

const rows = (root = "/w") => screen.queryAllByRole("treeitem", { name: new RegExp(`^chat-${root}-`) });
const more = () => screen.getByRole("button", { name: MORE });

beforeEach(() => {
  localStorage.setItem(STORAGE, "zh");
  boot();
});

afterEach(() => {
  cleanup();
  localStorage.removeItem(STORAGE);
  boot();
});

it("shows one batch and adds one batch on each show-more click", async () => {
  const total = 2 * BATCH + 2;
  draw([workspace("/w", total)]);
  expect(rows()).toHaveLength(BATCH);
  expect(more().dataset.action).toBe("workspace.sessions-more");
  expect(more().dataset.target).toBe("/w");
  expect(more().textContent).toBe(`还有 ${total - BATCH} 个 · 展开显示`);
  expect(more().getAttribute("aria-label")).toBe(more().textContent);
  await userEvent.click(more());
  expect(rows()).toHaveLength(2 * BATCH);
  expect(more().textContent).toBe("还有 2 个 · 展开显示");
  await userEvent.click(more());
  expect(rows()).toHaveLength(total);
  expect(screen.queryByRole("button", { name: MORE })).toBeNull();
});

it("keeps same-named projects independent and resets only the collapsed project", async () => {
  const otherCount = BATCH + 3;
  draw([workspace("/w", 2 * BATCH + 2), workspace("/other", otherCount)]);
  const buttons = () => screen.getAllByRole("button", { name: MORE });
  await userEvent.click(buttons()[0]);
  expect([rows().length, rows("/other").length]).toEqual([2 * BATCH, BATCH]);
  await userEvent.click(buttons()[1]);
  expect([rows().length, rows("/other").length]).toEqual([2 * BATCH, otherCount]);
  const project = () => screen.getAllByRole("treeitem", { name: /^project/ })[0];
  await userEvent.click(project());
  expect(rows()).toHaveLength(0);
  await userEvent.click(project());
  expect([rows().length, rows("/other").length]).toEqual([BATCH, otherCount]);
});

it.each([0, BATCH])("does not show a more button for %i sessions", (count) => {
  draw([workspace("/w", count)]);
  expect(rows()).toHaveLength(count);
  expect(screen.queryByRole("button", { name: MORE })).toBeNull();
});

it("resets to one batch after keyboard collapse and reopen", async () => {
  const total = BATCH + 4;
  draw([workspace("/w", total)]);
  await userEvent.click(more());
  expect(rows()).toHaveLength(total);
  screen.getByRole("treeitem", { name: /^project/ }).focus();
  await userEvent.keyboard("{ArrowLeft}");
  expect(rows()).toHaveLength(0);
  await userEvent.keyboard("{ArrowRight}");
  expect(rows()).toHaveLength(BATCH);
});

it("searches beyond the first page and still opens the matching session", async () => {
  const last = 2 * BATCH + 1;
  const { onOpen } = draw([workspace("/w", last + 1)]);
  await userEvent.type(screen.getByRole("searchbox"), `chat-/w-${last}`);
  expect(rows()).toHaveLength(1);
  await userEvent.click(rows()[0]);
  expect(onOpen).toHaveBeenCalledWith({ root: "/w", sessionPath: `/w/${last}.jsonl` });
  await userEvent.clear(screen.getByRole("searchbox"));
  expect(rows()).toHaveLength(BATCH);
});

it("paginates the filtered pinned sessions rather than the unfiltered tree", async () => {
  const ws = workspace("/w", 2 * BATCH + 2);
  const offset = BATCH - 1;
  const pinned = new Set(ws.sessions.slice(offset).map((session) => session.path));
  draw([ws], "pinned", pinned);
  expect(rows().map((row) => row.textContent)).toEqual(ws.sessions.slice(offset, offset + BATCH).map((session) => session.title));
  expect(more().textContent).toBe(`还有 ${pinned.size - BATCH} 个 · 展开显示`);
  await userEvent.click(more());
  expect(rows()).toHaveLength(pinned.size);
  expect(screen.queryByRole("button", { name: MORE })).toBeNull();
});

it("retains the page on tree reload and hides the button when no sessions remain hidden", async () => {
  const { relist } = draw([workspace("/w", 2 * BATCH + 2)]);
  await userEvent.click(more());
  relist([workspace("/w", 2 * BATCH + 3)]);
  expect(rows()).toHaveLength(2 * BATCH);
  relist([workspace("/w", BATCH + 2)]);
  expect(rows()).toHaveLength(BATCH + 2);
  expect(screen.queryByRole("button", { name: MORE })).toBeNull();
});

it("shows the English remaining count with an accessible name and the same interaction", async () => {
  localStorage.setItem(STORAGE, "en");
  boot();
  draw([workspace("/w", BATCH + 1)]);
  const button = screen.getByRole("button", { name: "1 more · Show more" });
  expect(button.getAttribute("aria-label")).toBe(button.textContent);
  expect(rows()).toHaveLength(BATCH);
  await userEvent.click(button);
  expect(rows()).toHaveLength(BATCH + 1);
  expect(screen.queryByRole("button", { name: /more · Show more/ })).toBeNull();
});

it("keeps the selected running session visible without counting it as hidden or duplicating it", async () => {
  const ws = workspace("/w", 2 * BATCH + 2);
  const selected = ws.sessions[2 * BATCH + 1];
  selected.runtimeId = "selected";
  const { onFocus } = draw([ws], "all", new Set(), "selected");
  const row = () => screen.getByRole("treeitem", { name: selected.title, selected: true });
  expect(rows()).toHaveLength(BATCH + 1);
  expect(row().dataset.run).toBe("running");
  expect(more().textContent).toBe(`还有 ${BATCH + 1} 个 · 展开显示`);
  await userEvent.click(row());
  expect(onFocus).toHaveBeenCalledWith("selected");
  await userEvent.click(more());
  expect(rows()).toHaveLength(2 * BATCH + 1);
  expect(more().textContent).toBe("还有 1 个 · 展开显示");
  const project = () => screen.getByRole("treeitem", { name: /^project/ });
  await userEvent.click(project());
  await userEvent.click(project());
  expect(rows()).toHaveLength(BATCH + 1);
  expect(row()).toBeTruthy();
  await userEvent.click(more());
  await userEvent.click(more());
  expect(rows()).toHaveLength(ws.sessions.length);
  expect(screen.getAllByRole("treeitem", { name: selected.title })).toHaveLength(1);
  expect(screen.queryByRole("button", { name: MORE })).toBeNull();
});

it("updates the extra visible row when selection moves beyond the first batch", () => {
  const ws = workspace("/w", 2 * BATCH + 2);
  ws.sessions[BATCH].runtimeId = "first";
  ws.sessions[2 * BATCH + 1].runtimeId = "second";
  const { relist } = draw([ws], "all", new Set(), "first");
  expect(screen.getByRole("treeitem", { selected: true }).textContent).toBe(ws.sessions[BATCH].title);
  relist([ws], "second");
  expect(rows()).toHaveLength(BATCH + 1);
  expect(screen.queryByRole("treeitem", { name: ws.sessions[BATCH].title })).toBeNull();
  expect(screen.getByRole("treeitem", { selected: true }).textContent).toBe(ws.sessions[2 * BATCH + 1].title);
});

it("does not reinsert a selected session excluded by the pinned scope", async () => {
  const ws = workspace("/w", 2 * BATCH + 2);
  const selected = ws.sessions[2 * BATCH + 1];
  selected.runtimeId = "selected";
  draw([ws], "pinned", new Set(ws.sessions.slice(0, BATCH + 1).map((session) => session.path)), "selected");
  expect(rows()).toHaveLength(BATCH);
  expect(screen.queryByRole("treeitem", { selected: true })).toBeNull();
  await userEvent.type(screen.getByRole("searchbox"), `chat-/w-${BATCH}`);
  expect(rows()).toHaveLength(1);
  expect(screen.queryByRole("treeitem", { selected: true })).toBeNull();
});

it("temporarily hides a selected session excluded by search and restores it when search is cleared", async () => {
  const ws = workspace("/w", 2 * BATCH + 2);
  ws.sessions[2 * BATCH + 1].runtimeId = "selected";
  draw([ws], "all", new Set(), "selected");
  await userEvent.type(screen.getByRole("searchbox"), "chat-/w-0");
  expect(rows()).toHaveLength(1);
  expect(screen.queryByRole("treeitem", { selected: true })).toBeNull();
  await userEvent.clear(screen.getByRole("searchbox"));
  expect(rows()).toHaveLength(BATCH + 1);
  expect(screen.getByRole("treeitem", { selected: true })).toBeTruthy();
});

it("hides the button when the selected extra row is the only session beyond the batch", () => {
  const ws = workspace("/w", BATCH + 1);
  ws.sessions[BATCH].runtimeId = "selected";
  draw([ws], "all", new Set(), "selected");
  expect(rows()).toHaveLength(ws.sessions.length);
  expect(screen.queryByRole("button", { name: MORE })).toBeNull();
});

it("keeps the opening selection visible across folding and restores the active selection after failure", async () => {
  const ws = workspace("/w", 2 * BATCH + 2);
  const active = ws.sessions[2 * BATCH + 1];
  active.runtimeId = "active";
  const opening = ws.sessions[BATCH];
  const { relist } = draw([ws], "all", new Set(), "active", opening.path);
  expect(rows()).toHaveLength(BATCH + 1);
  expect(screen.getByRole("treeitem", { name: opening.title, selected: true }).dataset.busy).toBe("");
  expect(screen.queryByRole("treeitem", { name: active.title })).toBeNull();
  await userEvent.click(more());
  expect(rows()).toHaveLength(2 * BATCH);
  const project = () => screen.getByRole("treeitem", { name: /^project/ });
  await userEvent.click(project());
  await userEvent.click(project());
  expect(rows()).toHaveLength(BATCH + 1);
  expect(screen.getByRole("treeitem", { name: opening.title, selected: true })).toBeTruthy();
  relist([ws], "active", "");
  expect(rows()).toHaveLength(BATCH + 1);
  expect(screen.getByRole("treeitem", { name: active.title, selected: true })).toBeTruthy();
  expect(screen.queryByRole("treeitem", { name: opening.title })).toBeNull();
});
