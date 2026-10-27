// @vitest-environment jsdom
import { afterEach, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import "./testkit";
import { Candidate } from "./AddPlugin";

afterEach(cleanup);

it("lists every frame document a package ships, with its size and digest, before the install", () => {
  render(<Candidate a={{
    kind: "plugin", action: "install_plugin_package", status: "planned", riskLevel: "high", name: "task-board",
    frames: [{ id: "board", title: "Task board", entry: "frames/board.html", bytes: 2467, sha256: "37aec0029a486ed74665800f4101d77a587c1d49aa82404a14b829dfc52061e1" }],
  }} />);
  const row = screen.getByText("界面文档").closest(".risk")!;
  expect(row.textContent).toContain("Task board");
  expect(row.textContent).toContain("frames/board.html");
  expect(row.textContent).toContain("2.4 KB");
  expect(row.textContent).toContain("37aec0029a48");
  expect(row.textContent).toContain("在隔离的沙箱里显示");
});

it("says when a declared frame document could not be read", () => {
  render(<Candidate a={{
    kind: "plugin", action: "install_plugin_package", status: "planned", riskLevel: "high", name: "task-board",
    frames: [{ id: "board", title: "Task board", entry: "frames/board.html", bytes: 0, error: "frame document is missing" }],
  }} />);
  expect(screen.getByText(/frame document is missing/)).toBeTruthy();
});
