// @vitest-environment jsdom
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import "./testkit";
import { WorkbenchPanel } from "./WorkbenchPanel";
import { useAgentRequest } from "./useAgentRequest";
import { MockPort } from "../port/mock";
import type { SubagentRun, SubagentTranscript } from "../port/subagent";

const runs: SubagentRun[] = [
  { ref: "sa_1", name: "audit", kind: "task", model: "m", state: "completed", parentCall: "call_a", startedAt: 1, updatedAt: 2 },
  { ref: "sa_2", name: "review", kind: "task", model: "m", state: "completed", parentCall: "call_b", startedAt: 1, updatedAt: 2 },
  { ref: "sa_3", name: "review", kind: "task", model: "m", state: "completed", parentCall: "call_b", startedAt: 1, updatedAt: 2 },
];

class Port extends MockPort {
  async subagents() { return runs; }
  async subagentTranscript(ref: string): Promise<SubagentTranscript> {
    const run = runs.find((r) => r.ref === ref)!;
    return { run, messages: [{ role: "assistant", content: `report of ${ref}`, msgIndex: 1 }], after: 0, next: 1, total: 1, live: false };
  }
}

let opened: boolean | undefined;
function Harness({ port }: { port: Port }) {
  const agents = useAgentRequest(port, () => {});
  return (
    <>
      <button onClick={async () => { opened = await agents.open("call_a"); }}>a</button>
      <button onClick={async () => { opened = await agents.open("call_b"); }}>b</button>
      <button onClick={async () => { opened = await agents.open("call_none"); }}>none</button>
      <WorkbenchPanel scheme="dark" shown changes={[]} onSurfaces={vi.fn()} onCloseManual={vi.fn()} onExternal={vi.fn()} manual tabs={[]} port={port} agentRequest={agents.request} />
    </>
  );
}

const tabs = () => [...document.querySelectorAll("[role=tab]")].map((e) => e.textContent?.trim());
const selected = () => document.querySelector("[role=tab][aria-selected=true]")?.textContent?.trim();
afterEach(cleanup);

describe("sub-agent runs as workbench tabs", () => {
  it("opens the run the card's call spawned and reads its record", async () => {
    render(<Harness port={new Port()} />);
    await userEvent.click(screen.getByText("a"));
    await waitFor(() => expect(tabs()).toEqual(["audit"]));
    expect(opened).toBe(true);
    expect(await screen.findByText(/report of sa_1/)).toBeTruthy();
  });

  it("opens every run of a call that spawned several, selecting the first, and shows the selected one", async () => {
    render(<Harness port={new Port()} />);
    await userEvent.click(screen.getByText("b"));
    await waitFor(() => expect(tabs()).toEqual(["review", "review"]));
    expect(selected()).toBe("review");
    expect(await screen.findByText(/report of sa_2/)).toBeTruthy();
  });

  it("does not open the same run twice, and lets several stay open at once", async () => {
    render(<Harness port={new Port()} />);
    await userEvent.click(screen.getByText("a"));
    await waitFor(() => expect(tabs()).toEqual(["audit"]));
    await userEvent.click(screen.getByText("a"));
    await act(async () => {});
    expect(tabs()).toEqual(["audit"]);
    await userEvent.click(screen.getByText("b"));
    await waitFor(() => expect(tabs()).toEqual(["audit", "review", "review"]));
  });

  it("opens a closed run again", async () => {
    render(<Harness port={new Port()} />);
    await userEvent.click(screen.getByText("a"));
    await waitFor(() => expect(tabs()).toEqual(["audit"]));
    await userEvent.click(document.querySelector<HTMLElement>(".workbench-tab-close")!);
    await waitFor(() => expect(tabs()).toEqual([]));
    await userEvent.click(screen.getByText("a"));
    await waitFor(() => expect(tabs()).toEqual(["audit"]));
  });

  it("reports false for a call with no run, and opens nothing", async () => {
    render(<Harness port={new Port()} />);
    await userEvent.click(screen.getByText("none"));
    await waitFor(() => expect(opened).toBe(false));
    expect(tabs()).toEqual([]);
  });

  it("reports false when the kernel cannot list runs", async () => {
    class Broken extends Port { async subagents(): Promise<SubagentRun[]> { throw new Error("404"); } }
    render(<Harness port={new Broken()} />);
    await userEvent.click(screen.getByText("a"));
    await waitFor(() => expect(opened).toBe(false));
  });
});
