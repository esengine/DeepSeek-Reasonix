// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "../testkit";
import { ToolCard } from "./ToolCard";
import { JobsLiveContext } from "../JobRunView";
import type { JobEntry } from "../../port/port";
import type { Tool } from "../../port/wire";

afterEach(cleanup);

const call: Tool = { id: "c1", name: "bash", readOnly: false, args: '{"command":"npm run test","run_in_background":true}', output: 'Started background job "bash-1".', execution: { kind: "shell", state: "background_started", jobId: "bash-1" } };
const live: JobEntry = { id: "bash-1", kind: "bash", label: "npm run test", status: "running", startedAt: Date.now() - 42_000 };

function row(jobs: JobEntry[], open = vi.fn()) {
  render(<JobsLiveContext.Provider value={{ jobs, open }}><div className="activity-body"><ToolCard tool={call} running={false} activity /></div></JobsLiveContext.Provider>);
  return open;
}

describe("a call that sent its command to the background", () => {
  it("says so, reads how long the job has run, and opens its output", async () => {
    const open = row([live]);
    expect(document.querySelector(".hl .nm")?.textContent).toBe("放到后台运行");
    expect(document.querySelector(".tool-bg")?.textContent).toBe("在跑 · 42 秒");
    expect(document.querySelector(".tool-bg-note")?.textContent).toContain("不等它跑完");
    await userEvent.click(screen.getByRole("button", { name: "看输出" }));
    expect(open).toHaveBeenCalledWith(live);
  });

  it("turns into how the job ended once it has, in the same row", () => {
    row([{ ...live, status: "done", startedAt: 1_000, endedAt: 742_000, exitCode: 0 }]);
    expect(document.querySelector(".tool-bg")?.textContent).toBe("12 分 21 秒 · 退出 0");
    expect(document.querySelector(".tool-bg-note")).toBeNull();
  });

  it("stays an ordinary row when the job is not in the pane's list", () => {
    row([]);
    expect(document.querySelector(".hl .nm")?.textContent).toBe("放到后台运行");
    expect(document.querySelector(".tool-bg")).toBeNull();
  });
});
