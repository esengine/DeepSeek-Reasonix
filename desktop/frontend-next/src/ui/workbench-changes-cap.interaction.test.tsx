// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { MockPort } from "../port/mock";
import type { AgentPort, WorkspaceChange } from "../port/port";
import { WorkbenchChanges } from "./WorkbenchChanges";

afterEach(cleanup);

const many = (n: number): WorkspaceChange[] =>
  Array.from({ length: n }, (_, i) => ({ path: `pkg${i % 40}/dir${i % 7}/file${i}.go`, status: "??", unstaged: true }));

function draw(list: WorkspaceChange[]) {
  const port = new MockPort() as unknown as AgentPort;
  port.stageChanges = vi.fn(async () => {});
  const utils = render(
    <WorkbenchChanges port={port} changes={list} current="" onOpen={() => {}} onChanged={() => {}}
      commit={{ button: <button data-action="commit.draft">提交…</button>, card: null }} />,
  );
  return { port, ...utils };
}

const rowCount = (root: HTMLElement) => root.querySelectorAll(".workbench-change-row").length;

describe("a workspace with very many changes", () => {
  it("draws a bounded number of rows and says how many it left out", () => {
    const { container } = draw(many(5000));
    expect(rowCount(container)).toBeLessThanOrEqual(600);
    expect(container.querySelector(".workbench-explorer-head small")?.textContent).toBe("5000");
    expect(screen.getByRole("button", { name: /再显示/ })).toBeTruthy();
    expect(container.textContent).toMatch(/还有 \d+ 项没有显示/);
  });

  it("shows more on request, and stages every change rather than only the drawn ones", async () => {
    const user = userEvent.setup();
    const { container, port } = draw(many(5000));
    const before = rowCount(container);
    await user.click(screen.getByRole("button", { name: /再显示/ }));
    expect(rowCount(container)).toBeGreaterThan(before);
    await user.click(screen.getByRole("button", { name: "暂存全部" }));
    expect(port.stageChanges).toHaveBeenCalledWith({ all: true, unstage: false });
  });

  it("draws a short list whole, with nothing left out", () => {
    const { container } = draw(many(30));
    expect(screen.queryByRole("button", { name: /再显示/ })).toBeNull();
    expect(container.querySelectorAll('[data-action-click="workbench.diff"]').length).toBe(30);
  });
});
