// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { DefaultMode } from "./DefaultMode";
import { MockPort } from "../port/mock";
import type { AgentPort } from "../port/port";

afterEach(cleanup);

const choice = (name: string) => screen.findByRole("radio", { name });

describe("the starting mode choice", () => {
  it("starts on Agent and saves the plan flag the choice implies", async () => {
    const p = new MockPort() as unknown as AgentPort;
    const save = vi.spyOn(p, "saveDefaultMode");
    render(<DefaultMode port={p} />);
    const agent = await choice("Agent");
    const plan = await choice("计划");
    expect(agent.getAttribute("aria-checked")).toBe("true");
    expect(plan.getAttribute("aria-checked")).toBe("false");
    await userEvent.click(plan);
    expect(save).toHaveBeenCalledWith(true);
    await waitFor(() => expect(plan.getAttribute("aria-checked")).toBe("true"));
    await userEvent.click(agent);
    expect(save).toHaveBeenLastCalledWith(false);
  });

  it("keeps the choice where it was when the save is refused", async () => {
    const p = new MockPort() as unknown as AgentPort;
    p.saveDefaultMode = async () => {
      throw new Error("disk full");
    };
    render(<DefaultMode port={p} />);
    await userEvent.click(await choice("计划"));
    await screen.findByText(/disk full/);
    expect((await choice("Agent")).getAttribute("aria-checked")).toBe("true");
  });

  it("says so when the setting cannot be read", async () => {
    const p = new MockPort() as unknown as AgentPort;
    p.defaultMode = async () => {
      throw new Error("offline");
    };
    render(<DefaultMode port={p} />);
    expect(await screen.findByText("无法读取新对话的起始模式设置。")).toBeTruthy();
  });
});
