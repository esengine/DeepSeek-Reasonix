// @vitest-environment jsdom
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MockPort } from "../port/mock";
import { WorkbenchHistory } from "./WorkbenchHistory";

afterEach(cleanup);

const tip = (id: string) => ({ repo: true, truncated: false, commits: [{ id, parents: [], subject: `commit ${id}`, author: "a", at: 0, refs: [] }] });

function mount(stamp: string) {
  const port = new MockPort();
  const log = vi.spyOn(port, "workspaceGitLog").mockResolvedValue(tip("aaaaaaa1111"));
  const view = render(<WorkbenchHistory port={port} branch="main" stamp={stamp} />);
  return { port, log, view };
}

describe("the commit history of an open section", () => {
  it("reads once when opened and not again while nothing moved", async () => {
    const { port, log, view } = mount("0:0");
    await screen.findByText("commit aaaaaaa1111");
    view.rerender(<WorkbenchHistory port={port} branch="main" stamp="0:0" />);
    expect(log).toHaveBeenCalledTimes(1);
  });

  it("shows a commit made after it was opened, without a reload and without blanking the list", async () => {
    const { port, log, view } = mount("0:0");
    await screen.findByText("commit aaaaaaa1111");
    log.mockResolvedValue(tip("bbbbbbb2222"));
    view.rerender(<WorkbenchHistory port={port} branch="main" stamp="1:0" />);
    expect(screen.queryByText("commit aaaaaaa1111")).not.toBeNull();
    await waitFor(() => expect(screen.queryByText("commit bbbbbbb2222")).not.toBeNull());
    expect(log).toHaveBeenCalledTimes(2);
    expect(screen.queryByRole("status")).toBeNull();
  });

  it("reads again when the branch moves", async () => {
    const { port, log, view } = mount("0:0");
    await screen.findByText("commit aaaaaaa1111");
    await act(async () => view.rerender(<WorkbenchHistory port={port} branch="topic" stamp="0:0" />));
    expect(log).toHaveBeenCalledTimes(2);
  });

  it("keeps the list it has when a quiet re-read fails", async () => {
    const { port, log, view } = mount("0:0");
    await screen.findByText("commit aaaaaaa1111");
    log.mockRejectedValue(new Error("busy"));
    await act(async () => view.rerender(<WorkbenchHistory port={port} branch="main" stamp="1:0" />));
    expect(screen.queryByText("commit aaaaaaa1111")).not.toBeNull();
    expect(screen.queryByRole("alert")).toBeNull();
  });
});
