// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { SubagentClaims } from "./SubagentClaims";
import { MockPort } from "../port/mock";
import type { AgentPort } from "../port/port";

afterEach(cleanup);

const toggle = () => screen.findByRole("switch", { name: "乐观模式（效率优先）" });

describe("the sub-agent claims switch", () => {
  it("starts on, says what the optimistic mode costs, and saves the opposite of what it shows", async () => {
    const p = new MockPort() as unknown as AgentPort;
    const save = vi.spyOn(p, "saveSubagentClaims");
    const changed = vi.fn();
    render(<SubagentClaims port={p} onChanged={changed} />);
    const sw = await toggle();
    expect(sw.getAttribute("aria-checked")).toBe("true");
    expect(screen.getByText(/不受路径围栏约束，只在事后观察/)).toBeTruthy();
    await userEvent.click(sw);
    expect(save).toHaveBeenCalledWith(false);
    await waitFor(() => expect(sw.getAttribute("aria-checked")).toBe("false"));
    expect(changed).toHaveBeenCalledTimes(1);
  });

  it("keeps the switch where it was when the save is refused", async () => {
    const p = new MockPort() as unknown as AgentPort;
    p.saveSubagentClaims = async () => {
      throw new Error("disk full");
    };
    const changed = vi.fn();
    render(<SubagentClaims port={p} onChanged={changed} />);
    const sw = await toggle();
    await userEvent.click(sw);
    await screen.findByText(/disk full/);
    expect(sw.getAttribute("aria-checked")).toBe("true");
    expect(changed).not.toHaveBeenCalled();
  });

  it("says so when the setting cannot be read", async () => {
    const p = new MockPort() as unknown as AgentPort;
    p.subagentClaims = async () => {
      throw new Error("offline");
    };
    render(<SubagentClaims port={p} onChanged={() => {}} />);
    expect(await screen.findByText("无法读取子代理写入范围设置。")).toBeTruthy();
  });
});
