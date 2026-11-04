// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { WorkbenchHistory } from "./WorkbenchHistory";
import { MockPort } from "../port/mock";
import { HttpError } from "../port/http_error";
import type { AgentPort } from "../port/port";
import type { GitHistory } from "../port/workspace";

afterEach(cleanup);

const open = async () => {
  if (!document.querySelector<HTMLDetailsElement>("details.workbench-history")!.open) await userEvent.click(screen.getByText("提交历史"));
};

it("is open from the start, as the board draws it, and draws one row per commit with its hash and refs", async () => {
  const port = new MockPort();
  const read = vi.spyOn(port, "workspaceGitLog");
  render(<WorkbenchHistory port={port as AgentPort} branch="main" />);
  expect(document.querySelector(".gh-branch")!.textContent).toBe("main");
  await waitFor(() => expect(screen.getByText("feat(studio): show the commit graph")).toBeTruthy());
  expect(read).toHaveBeenCalledOnce();
  expect(document.querySelectorAll(".gh-row")).toHaveLength(7);
  expect(screen.getByText("HEAD -> main")).toBeTruthy();
  expect(screen.getByText("tag: v2.38.0")).toBeTruthy();
  // The merge row has two lines leaving it, so its graph column draws two lanes.
  const merge = screen.getByText("Merge branch 'retry-backoff'").closest(".gh-row")!;
  expect(merge.querySelectorAll("svg path").length).toBeGreaterThanOrEqual(2);
  expect(merge.querySelector("svg circle")).toBeTruthy();
  expect(merge.querySelector(".gh-meta")?.getAttribute("title")).toMatch(/ada/);
  expect(merge.querySelector(".gh-hash")?.textContent).toBe("a2");
});

it("says so for a folder that is not under git, and offers no graph", async () => {
  const port = new MockPort();
  vi.spyOn(port, "workspaceGitLog").mockResolvedValue({ repo: false, commits: [], truncated: false });
  render(<WorkbenchHistory port={port as AgentPort} />);
  await open();
  await waitFor(() => expect(screen.getByText("这个文件夹不是 git 仓库")).toBeTruthy());
  expect(document.querySelector(".gh-row")).toBeNull();
});

it("says why a read failed and reads again on request", async () => {
  const port = new MockPort();
  const read = vi.spyOn(port, "workspaceGitLog").mockRejectedValueOnce(new HttpError(500, "boom", { code: "git.history_failed", error: "git refused" }, true));
  render(<WorkbenchHistory port={port as AgentPort} />);
  await open();
  await waitFor(() => expect(screen.getByRole("alert")).toBeTruthy());
  read.mockResolvedValue(await new MockPort().workspaceGitLog());
  await userEvent.click(screen.getByRole("button", { name: "重新读取" }));
  await waitFor(() => expect(screen.getByText("docs: describe the wire log")).toBeTruthy());
  expect(read).toHaveBeenCalledTimes(2);
});

it("tells the reader the history goes on past the page", async () => {
  const port = new MockPort();
  const page: GitHistory = { ...(await new MockPort().workspaceGitLog()), truncated: true };
  vi.spyOn(port, "workspaceGitLog").mockResolvedValue(page);
  render(<WorkbenchHistory port={port as AgentPort} />);
  await open();
  await waitFor(() => expect(screen.getByText("只显示最近的提交")).toBeTruthy());
});

it("a repository with no commit yet shows an empty history, not an error", async () => {
  const port = new MockPort();
  vi.spyOn(port, "workspaceGitLog").mockResolvedValue({ repo: true, commits: [], truncated: false });
  render(<WorkbenchHistory port={port as AgentPort} />);
  await open();
  await waitFor(() => expect(screen.getByText("还没有提交")).toBeTruthy());
  expect(screen.queryByRole("alert")).toBeNull();
});
