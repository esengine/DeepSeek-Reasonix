// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Composer } from "./Composer";
import { MockPort } from "../port/mock";
import { boot, STORAGE } from "../i18n";
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
    expect(refine.getAttribute("aria-disabled")).toBe("true");
    fireEvent.change(box, { target: { value: "修一下那个bug" } });
    expect(refine.getAttribute("aria-disabled")).toBe("false");
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
    expect(refine.getAttribute("aria-disabled")).toBe("false");
  });

  it("describes itself by its tooltip, so the shortcut is read out", () => {
    const { refine } = draw(async () => "x");
    const tip = refine.querySelector('[role="tooltip"]') as HTMLElement;
    expect(tip.id).not.toBe("");
    expect(refine.getAttribute("aria-describedby")).toBe(tip.id);
    expect(tip.textContent).toContain("Ctrl+Shift+E");
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

  it("cancels a pending keyboard request and ignores its late result", async () => {
    let finish!: (text: string) => void;
    let signal!: AbortSignal;
    const { container, box, refine } = draw((_draft, current) => new Promise((resolve) => { signal = current!; finish = resolve; }));
    fireEvent.change(box, { target: { value: "draft" } });
    fireEvent.keyDown(box, { key: "E", ctrlKey: true, shiftKey: true });
    expect(refine.getAttribute("aria-disabled")).toBe("true");
    fireEvent.keyDown(box, { key: "Escape" });
    expect(signal.aborted).toBe(true);
    finish("late rewrite");
    await waitFor(() => expect(refine.getAttribute("aria-disabled")).toBe("false"));
    expect(container.querySelector(".refine-card")).toBeNull();
    expect(box.value).toBe("draft");
  });
});

describe("the refine button's tooltip", () => {
  it.each([
    ["zh", "用当前模型改写得更清楚，采用前不会替换原文"],
    ["en", "Rewrite it more clearly with the current model; your text stays until you adopt it"],
  ])("keeps the separator with the shortcut in %s without losing the description", (language, description) => {
    const saved = localStorage.getItem(STORAGE);
    try {
      localStorage.setItem(STORAGE, language);
      boot();
      const { refine, getByText } = draw(async () => "x");
      const tip = refine.querySelector('[role="tooltip"]') as HTMLElement;
      const shortcut = getByText("· Ctrl+Shift+E", { exact: true });
      expect(window.getComputedStyle(shortcut).whiteSpace).toBe("nowrap");
      expect(tip.contains(shortcut)).toBe(true);
      expect(tip.textContent).toContain(`${description} · Ctrl+Shift+E`);
      expect(refine.getAttribute("aria-describedby")).toBe(tip.id);
    } finally {
      if (saved === null) localStorage.removeItem(STORAGE);
      else localStorage.setItem(STORAGE, saved);
      boot();
    }
  });

  it("is reachable by keyboard with an empty draft without starting a request", async () => {
    const { refine, port } = draw(async () => "x");
    await userEvent.tab();
    await userEvent.tab();
    await userEvent.tab();
    expect(document.activeElement).toBe(refine);
    expect(refine.getAttribute("aria-disabled")).toBe("true");
    await userEvent.keyboard("{Enter} ");
    expect(port.refinePrompt).not.toHaveBeenCalled();
  });

  it("keeps keyboard help available while working and suppresses repeat activation", async () => {
    let finish!: (text: string) => void;
    const { refine, box, port, container } = draw(() => new Promise((resolve) => { finish = resolve; }));
    fireEvent.change(box, { target: { value: "draft" } });
    await userEvent.click(refine);
    box.focus();
    refine.focus();
    expect(document.activeElement).toBe(refine);
    expect(refine.getAttribute("aria-disabled")).toBe("true");
    expect(refine.getAttribute("aria-busy")).toBe("true");
    await userEvent.keyboard("{Enter} ");
    await userEvent.click(refine);
    expect(port.refinePrompt).toHaveBeenCalledTimes(1);
    finish("rewritten");
    await waitFor(() => expect(container.querySelector(".refine-text")).toBeTruthy());
    expect(refine.getAttribute("aria-disabled")).toBe("false");
  });

  it("is still rendered while the button is disabled, so the control explains itself", () => {
    const { refine } = draw(async () => "x");
    expect(refine.getAttribute("aria-disabled")).toBe("true");
    const tip = refine.querySelector('[role="tooltip"]');
    expect(tip?.textContent).toContain("Ctrl+Shift+E");
  });
});
