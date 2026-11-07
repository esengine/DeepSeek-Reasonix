// @vitest-environment jsdom
import { cleanup, fireEvent, render, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import "./testkit";
import { Composer } from "./Composer";
import { MockPort } from "../port/mock";
import type { AgentPort, ApprovalMode, ModelEntry, Preset, SessionStatus } from "../port/port";

afterEach(() => { cleanup(); localStorage.clear(); vi.restoreAllMocks(); });

const MODELS: ModelEntry[] = [
  { ref: "deepseek/deepseek-v4-pro", provider: "deepseek", model: "deepseek-v4-pro", efforts: ["auto", "low", "high", "max"] },
  { ref: "deepseek/deepseek-flash", provider: "deepseek", model: "deepseek-flash", efforts: ["auto", "low"] },
];
const status = (over: Partial<SessionStatus> = {}) =>
  ({ preset: "balanced" as Preset, effort: "auto", toolApprovalMode: "ask" as ApprovalMode, plan: false, modelRef: "deepseek/deepseek-v4-pro", ...over }) as SessionStatus;

function draw(st = status(), port = new MockPort()) {
  vi.spyOn(port, "models").mockResolvedValue(MODELS);
  const view = render(<Composer port={port as unknown as AgentPort} status={st} running={false} focus={0} onSubmit={vi.fn(async () => true)} onChanged={vi.fn()} onError={vi.fn()} />);
  return { ...view, port };
}
const trigger = (c: HTMLElement) => c.querySelector(".model-picker") as HTMLElement;
const open = async (c: HTMLElement) => {
  await waitFor(() => expect(document.querySelector('[data-action="model.select"]')).toBeTruthy());
  fireEvent.click(trigger(c));
};

describe("the model and its reasoning effort share one button and one panel", () => {
  it("draws a single trigger that reads the model and the effort once", async () => {
    const { container } = draw(status({ effort: "high" }));
    await waitFor(() => expect(trigger(container).textContent).toContain("深入"));
    expect(trigger(container).textContent).toContain("deepseek-v4-pro");
    expect(trigger(container).textContent).not.toContain("High");
    expect(container.querySelectorAll(".studio-effort-picker, .model-picker")).toHaveLength(1);
  });

  it("opens the models on the left and the efforts of the current model beside them", async () => {
    const { container } = draw();
    await open(container);
    const aside = document.querySelector(".portmenu .menu-aside")!;
    expect([...aside.querySelectorAll('[data-action="reasoning.effort"]')].map((e) => e.getAttribute("data-value"))).toEqual(["auto", "low", "high", "max"]);
    expect(aside.textContent).toContain("deepseek-v4-pro");
  });

  it("applies an effort and closes the panel", async () => {
    const port = new MockPort();
    const set = vi.spyOn(port, "setEffort");
    const { container } = draw(status(), port);
    await open(container);
    fireEvent.click(document.querySelector('.menu-aside [data-value="high"]')!);
    await waitFor(() => expect(set).toHaveBeenCalledWith("high"));
    expect((document.querySelector(".portmenu.has-aside") as HTMLElement).hidden).toBe(true);
  });

  it("applies a model and leaves the panel open so an effort can follow", async () => {
    const port = new MockPort();
    const set = vi.spyOn(port, "setModel");
    const { container } = draw(status(), port);
    await open(container);
    fireEvent.click(document.querySelector('.portmenu [data-action="model.select"][data-value="deepseek/deepseek-flash"]')!);
    await waitFor(() => expect(set).toHaveBeenCalledWith("deepseek/deepseek-flash"));
    expect((document.querySelector(".portmenu.has-aside") as HTMLElement).hidden).toBe(false);
  });

  it("points at where levels are declared when the model reports none", async () => {
    const port = new MockPort();
    vi.spyOn(port, "models").mockResolvedValue([{ ref: "relay/x", provider: "relay", model: "x" }]);
    const { container } = render(<Composer port={port as unknown as AgentPort} status={status({ modelRef: "relay/x" })} running={false} focus={0} onSubmit={vi.fn(async () => true)} onChanged={vi.fn()} onError={vi.fn()} />);
    await waitFor(() => expect(document.querySelector('[data-action="model.select"]')).toBeTruthy());
    fireEvent.click(trigger(container));
    expect(document.querySelector(".menu-aside")?.textContent).toContain("声明推理档位");
    expect(trigger(container).textContent).toContain("未声明");
  });
});
