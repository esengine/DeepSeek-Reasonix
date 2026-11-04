// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, renderHook, screen, waitFor } from "@testing-library/react";
import "./testkit";
import { JobRunView, JobsLiveContext } from "./JobRunView";
import { DeckChips } from "./DeckChips";
import { useAgentTabs } from "./useAgentTabs";
import type { JobEntry, JobOutput } from "../port/port";

afterEach(cleanup);

const running: JobEntry = { id: "bash-1", kind: "bash", label: "npm run test", description: "跑桌面端测试", detail: "npm run test 2>&1 | tail -40", status: "running", startedAt: Date.now() - 741_000 };
const failed: JobEntry = { ...running, id: "bash-2", status: "failed", startedAt: 1_000, endedAt: 243_000, exitCode: 1 };
const port = (out: Partial<JobOutput>) => ({ jobOutput: vi.fn(async () => ({ text: "", status: "running", bytes: 0, truncated: false, ...out }) as JobOutput) });

describe("a background job's workbench tab", () => {
  it("shows the command and what it printed, and stops it through the pane while it runs", async () => {
    const cancel = vi.fn(async () => {});
    const p = port({ text: "compiling module 192 … ok\n" });
    render(<JobsLiveContext.Provider value={{ jobs: [running], cancel }}><JobRunView port={p} id="bash-1" name="跑桌面端测试" shown /></JobsLiveContext.Provider>);
    await waitFor(() => expect(document.querySelector(".jobrun-out")?.textContent).toContain("compiling module 192 … ok"));
    expect(document.querySelector(".jobrun-cmd")?.textContent).toContain("$ npm run test 2>&1 | tail -40");
    expect(document.querySelector(".jobrun-state")?.textContent).toBe("12 分 21 秒");
    fireEvent.click(screen.getByRole("button", { name: "停止" }));
    expect(cancel).toHaveBeenCalledWith("bash-1");
  });

  it("says how an ended job finished and offers no stop", async () => {
    render(<JobsLiveContext.Provider value={{ jobs: [failed] }}><JobRunView port={port({ status: "failed", text: "FAIL\n" })} id="bash-2" name="x" shown /></JobsLiveContext.Provider>);
    await waitFor(() => expect(document.querySelector(".jobrun")?.getAttribute("data-status")).toBe("failed"));
    expect(document.querySelector(".jobrun-state")?.textContent).toBe("跑完了，退出 1 · 4 分 02 秒");
    expect(screen.queryByRole("button", { name: "停止" })).toBeNull();
  });

  it("stops following when the reader scrolls up, and the arrow brings it back", async () => {
    render(<JobsLiveContext.Provider value={{ jobs: [running] }}><JobRunView port={port({ text: "a\n".repeat(200) })} id="bash-1" name="x" shown /></JobsLiveContext.Provider>);
    const out = await waitFor(() => document.querySelector<HTMLPreElement>(".jobrun-out")!);
    Object.defineProperty(out, "scrollHeight", { value: 4000, configurable: true });
    Object.defineProperty(out, "clientHeight", { value: 400, configurable: true });
    out.scrollTop = 100;
    fireEvent.scroll(out);
    expect(document.querySelector(".jobrun-foot")?.textContent).toContain("向上滚就停在那里");
    fireEvent.click(screen.getByRole("button", { name: "回到最新输出" }));
    expect(document.querySelector(".jobrun-foot")?.textContent).toContain("跟着最新输出");
    expect(out.scrollTop).toBe(4000);
  });
});

describe("opening a job's output", () => {
  it("opens it in the workbench rather than over the conversation when the pane can", () => {
    const onOpenJob = vi.fn();
    const box = render(<DeckChips tasks={[]} jobs={[running]} open="jobs" onOpen={vi.fn()} onReadJob={vi.fn()} onOpenJob={onOpenJob} />).container;
    (box.querySelector('[data-action="job.view"]') as HTMLButtonElement).click();
    expect(onOpenJob).toHaveBeenCalledWith(running);
    expect(document.querySelector(".agent-tx")).toBeNull();
  });

  it("opens one tab per job however often it is asked", () => {
    const pick = vi.fn();
    const dismiss = vi.fn();
    const { result, rerender } = renderHook(({ req }) => useAgentTabs(req, pick, dismiss), { initialProps: { req: { jobs: [{ id: "bash-1", name: "x" }] } } });
    act(() => rerender({ req: { jobs: [{ id: "bash-1", name: "x" }] } }));
    expect(result.current.agents).toEqual([{ kind: "job", id: "bash-1", name: "x" }]);
    expect(pick).toHaveBeenLastCalledWith("job:bash-1");
  });
});
