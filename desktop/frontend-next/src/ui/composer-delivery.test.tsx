// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import "./testkit";
import { Composer } from "./Composer";
import { MockPort } from "../port/mock";
import type { AgentPort, ApprovalMode, Preset, SessionStatus } from "../port/port";
import { draftKey } from "./drafts";

afterEach(() => { cleanup(); localStorage.clear(); vi.restoreAllMocks(); });

const status = (over: Partial<SessionStatus> = {}) =>
  ({
    preset: "balanced" as Preset,
    effort: "auto",
    toolApprovalMode: "ask" as ApprovalMode,
    plan: false,
    modelRef: "deepseek/deepseek-v4-pro",
    ...over,
  }) as SessionStatus;

// The kernel's delivery window: the turn has ended, so `running` is false, and
// admission is still shut while its TurnDone lands, so `busy` is true. It is
// the one state `running` alone cannot describe.
const delivering = (): SessionStatus => ({ ...status({ running: false }), busy: true }) as SessionStatus;

function draw(over: { running?: boolean; st?: SessionStatus } = {}) {
  const port = new MockPort();
  const st = over.st ?? status();
  const onSubmit = vi.fn(async () => true);
  const view = render(
    <Composer
      port={port as unknown as AgentPort}
      status={st}
      running={over.running ?? false}
      focus={0}
      onSubmit={onSubmit}
      onChanged={vi.fn()}
      onError={vi.fn()}
      draftKey={draftKey("", st.workspaceRoot ?? "/workspace", st.sessionPath ?? "")}
    />,
  );
  const box = view.container.querySelector('textarea[aria-label="任务输入"]') as HTMLTextAreaElement;
  const type = (text: string) => fireEvent.change(box, { target: { value: text, selectionStart: text.length } });
  return { ...view, port, onSubmit, box, type };
}

describe("the composer during the delivery of a finished turn", () => {
  it("says the line will be queued rather than pretending the session is idle", async () => {
    const { type, onSubmit } = draw({ st: delivering() });
    type("再检查一遍");
    const send = screen.getByRole("button", { name: "排队发送" });
    expect(send.dataset.delivering).toBe("");
    // The turn is over, so there is nothing to stop; the click still sends, and
    // the kernel answers it by queueing behind the landing turn.
    expect(screen.queryByRole("button", { name: "停下" })).toBeNull();
    fireEvent.click(send);
    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith("再检查一遍"));
  });

  it("keeps the plain send when an older kernel sends no busy", () => {
    const { type } = draw({ st: status({ running: false }) });
    type("再检查一遍");
    const send = screen.getByRole("button", { name: "发送" });
    expect(send.hasAttribute("data-delivering")).toBe(false);
    expect(screen.queryByRole("button", { name: "排队发送" })).toBeNull();
    expect(screen.queryByRole("button", { name: "停下" })).toBeNull();
  });

  it("keeps the steer wording and the stop control while a turn is live", () => {
    const { type } = draw({ running: true, st: status({ running: true, busy: true }) });
    type("补充一句");
    expect(screen.getByRole("button", { name: "插话" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "停下" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "排队发送" })).toBeNull();
  });

  it("keeps an idle composer exactly as it was", () => {
    const { type, onSubmit } = draw({ st: status({ running: false, busy: false }) });
    const idle = screen.getByRole("button", { name: "发送" }) as HTMLButtonElement;
    expect(idle.disabled).toBe(true);
    expect(idle.hasAttribute("data-delivering")).toBe(false);
    type("描述一件事");
    expect(idle.disabled).toBe(false);
    fireEvent.click(idle);
    return waitFor(() => expect(onSubmit).toHaveBeenCalledWith("描述一件事"));
  });
});
