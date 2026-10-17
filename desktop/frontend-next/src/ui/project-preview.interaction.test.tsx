// @vitest-environment jsdom
import "./testkit";
import { useState } from "react";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it } from "vitest";
import { Workspaces } from "./Workspaces";
import { RailSearch } from "./railsearch";

afterEach(cleanup);

function Preview() {
  const [folded, setFolded] = useState(new Set<string>());
  return <RailSearch><Workspaces hub={{} as never} tree={[{ root: "/w", name: "Project",
    sessions: Array.from({ length: 8 }, (_, i) => ({ path: "/s/" + i, name: "Task " + i, turns: 1,
      lastActiveAt: Date.now() - 60_000 * (i + 1), runtimeId: i === 7 ? "active" : undefined })) }]}
    treeRead runtimes={[]} active="active" previewLimit={5} folded={folded}
    onFold={(root, shut) => setFolded(shut ? new Set([root]) : new Set())} reload={async () => {}}
    onOpen={async () => {}} onFocus={() => {}} onClose={async () => {}} liveIds={() => []} runs={{}}
    onRename={() => {}} onError={() => {}} adder={{ add: () => {} } as never} /></RailSearch>;
}

it("previews five recent conversations and keeps an older active conversation reachable", async () => {
  render(<Preview />);
  expect(screen.getAllByText(/^Task \d$/)).toHaveLength(6);
  expect(screen.getByText("Task 7")).toBeTruthy();
  expect(document.querySelectorAll("time")).toHaveLength(6);
  await userEvent.click(screen.getByRole("button", { name: "显示更多（还有 2 个）" }));
  expect(screen.getAllByText(/^Task \d$/)).toHaveLength(8);
});

it("folds from the folder title without a separate arrow, and reveals search matches", async () => {
  render(<Preview />);
  expect(document.querySelector(".wsrow .twist")).toBeNull();
  await userEvent.click(screen.getByText("Project"));
  expect(screen.queryByText("Task 0")).toBeNull();
  await userEvent.type(screen.getByRole("searchbox", { name: "搜索会话 / 项目" }), "Task 6");
  expect(screen.getByText("Task 6")).toBeTruthy();
});
