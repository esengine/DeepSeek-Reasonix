// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, waitFor } from "@testing-library/react";
import "./testkit";
import { Composer } from "./Composer";
import { MockPort } from "../port/mock";
import { HttpError, type AgentPort, type ApprovalMode, type Preset, type SessionStatus } from "../port/port";

afterEach(cleanup);

const status = {
  preset: "balanced" as Preset, effort: "auto", toolApprovalMode: "ask" as ApprovalMode,
  plan: false, modelRef: "deepseek/deepseek-v4-pro",
} as SessionStatus;

function draw(refinePrompt: AgentPort["refinePrompt"]) {
  const port = new MockPort() as unknown as AgentPort;
  port.refinePrompt = vi.fn(refinePrompt);
  const r = render(
    <Composer port={port} status={status} running={false} focus={0} onSubmit={vi.fn()} onChanged={vi.fn()} onError={vi.fn()} />,
  );
  const box = r.container.querySelector("textarea") as HTMLTextAreaElement;
  const refine = r.container.querySelector('.turntools [data-action="prompt.refine"]') as HTMLButtonElement;
  return { ...r, port, box, refine };
}

describe("refining a prompt before it is sent", () => {
  it("asks only when there is a draft", () => {
    const { refine, box } = draw(async () => "x");
    expect(refine.disabled).toBe(true);
    fireEvent.change(box, { target: { value: "修一下那个bug" } });
    expect(refine.disabled).toBe(false);
  });

  // The rewrite is offered beside the draft; the draft is only replaced when
  // the person takes it.
  it("shows the rewrite and replaces the draft only when adopted", async () => {
    const { container, box, refine, port } = draw(async () => "修复登录后跳回登录页的问题");
    fireEvent.change(box, { target: { value: "修一下那个bug" } });
    fireEvent.click(refine);
    expect(port.refinePrompt).toHaveBeenCalledWith("修一下那个bug", expect.any(AbortSignal));
    await waitFor(() => expect(container.querySelector(".refine-text")?.textContent).toBe("修复登录后跳回登录页的问题"));
    expect(box.value).toBe("修一下那个bug");

    fireEvent.click(container.querySelector('[data-action="prompt.adopt"]')!);
    expect(box.value).toBe("修复登录后跳回登录页的问题");
    expect(container.querySelector(".refine-card")).toBeNull();
  });

  it("leaves the draft alone when the rewrite is discarded", async () => {
    const { container, box, refine } = draw(async () => "rewritten");
    fireEvent.change(box, { target: { value: "draft" } });
    fireEvent.click(refine);
    await waitFor(() => expect(container.querySelector(".refine-text")).toBeTruthy());
    fireEvent.click(container.querySelector('[data-action="prompt.discard"]')!);
    expect(box.value).toBe("draft");
    expect(container.querySelector(".refine-card")).toBeNull();
  });

  // A refusal reaches the reader in their language, from its code.
  it("says why a rewrite could not be made", async () => {
    const { container, box, refine } = draw(async () => {
      throw new HttpError(409, "no model", { code: "prompt_refine.no_model", error: "no model" }, true);
    });
    fireEvent.change(box, { target: { value: "draft" } });
    fireEvent.click(refine);
    await waitFor(() => expect(container.querySelector(".refine-error")?.textContent).toBe("当前会话没有可用的模型，无法优化提示词"));
  });

  it("answers the keyboard: Ctrl+Shift+E asks, Escape puts the card away", async () => {
    const { container, box, port } = draw(async () => "rewritten");
    fireEvent.change(box, { target: { value: "draft" } });
    fireEvent.keyDown(box, { key: "E", ctrlKey: true, shiftKey: true });
    expect(port.refinePrompt).toHaveBeenCalledTimes(1);
    await waitFor(() => expect(container.querySelector(".refine-text")).toBeTruthy());
    fireEvent.keyDown(box, { key: "Escape" });
    expect(container.querySelector(".refine-card")).toBeNull();
  });
});
