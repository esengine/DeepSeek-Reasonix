// @vitest-environment jsdom
import { afterEach, expect, it } from "vitest";
import { useState } from "react";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Workspaces } from "./Workspaces";

afterEach(cleanup);

it("shows five sessions and adds five on each show-more click", async () => {
  const sessions = Array.from({ length: 12 }, (_, i) => ({ path: `/w/${i}.jsonl`, name: `chat-${i}`, title: `chat-${i}` }));
  render(<Workspaces hub={{} as never} tree={[{ root: "/w", name: "project", sessions }]} treeRead
    runtimes={[]} active="" folded={new Set()} onFold={() => {}} reload={async () => {}}
    onOpen={async () => {}} onFocus={() => {}} onClose={async () => {}} liveIds={() => []}
    runs={{}} onRename={() => {}} onError={() => {}} adder={{ add: () => {}, close: () => {}, at: null } as never} />);
  const rows = () => screen.getAllByRole("treeitem").filter(row => row.textContent?.startsWith("chat-"));
  expect(rows()).toHaveLength(5);
  await userEvent.click(screen.getByRole("button", { name: "展开显示" }));
  expect(rows()).toHaveLength(10);
  await userEvent.click(screen.getByRole("button", { name: "展开显示" }));
  expect(rows()).toHaveLength(12);
  expect(screen.queryByRole("button", { name: "展开显示" })).toBeNull();
});

it("returns to the first five sessions after collapsing and reopening a project", async () => {
  const sessions = Array.from({ length: 9 }, (_, i) => ({ path: `/w/${i}.jsonl`, name: `chat-${i}`, title: `chat-${i}` }));
  function Fixture() {
    const [folded, setFolded] = useState(new Set<string>());
    return <Workspaces hub={{} as never} tree={[{ root: "/w", name: "project", sessions }]} treeRead
      runtimes={[]} active="" folded={folded} onFold={(root, closed) => setFolded((prev) => {
        const next = new Set(prev);
        if (closed) next.add(root); else next.delete(root);
        return next;
      })} reload={async () => {}} onOpen={async () => {}} onFocus={() => {}} onClose={async () => {}}
      liveIds={() => []} runs={{}} onRename={() => {}} onError={() => {}}
      adder={{ add: () => {}, close: () => {}, at: null } as never} />;
  }
  render(<Fixture />);
  const rows = () => screen.getAllByRole("treeitem").filter(row => row.textContent?.startsWith("chat-"));
  const project = () => screen.getByRole("treeitem", { name: /^project/ });
  await userEvent.click(screen.getByRole("button", { name: "展开显示" }));
  expect(rows()).toHaveLength(9);
  await userEvent.click(project());
  expect(rows()).toHaveLength(0);
  await userEvent.click(project());
  expect(rows()).toHaveLength(5);
  expect(screen.getByRole("button", { name: "展开显示" })).toBeTruthy();
});
