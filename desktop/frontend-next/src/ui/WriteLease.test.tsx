// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { WriteLease } from "./WriteLease";
import { MockPort } from "../port/mock";
import type { AgentPort } from "../port/port";

afterEach(cleanup);

const toggle = () => screen.findByRole("switch", { name: "乐观模式（多会话并行）" });

describe("the write-lease switch", () => {
  it("starts on, says what it costs and when it takes effect, and saves the opposite of what it shows", async () => {
    const p = new MockPort() as unknown as AgentPort;
    const save = vi.spyOn(p, "saveWriteLease");
    render(<WriteLease port={p} />);
    const sw = await toggle();
    expect(sw.getAttribute("aria-checked")).toBe("true");
    expect(screen.getByText(/没有事前的路径围栏，只在事后观察/)).toBeTruthy();
    expect(screen.getByText(/只对之后新开的会话生效/)).toBeTruthy();
    await userEvent.click(sw);
    expect(save).toHaveBeenCalledWith({ optimistic: false });
    await waitFor(() => expect(sw.getAttribute("aria-checked")).toBe("false"));
    await userEvent.click(sw);
    expect(save).toHaveBeenLastCalledWith({ optimistic: true });
  });

  it("shows the longest wait, saves a changed one on blur and shows what the kernel stored", async () => {
    const p = new MockPort() as unknown as AgentPort;
    const save = vi.spyOn(p, "saveWriteLease");
    render(<WriteLease port={p} />);
    const box = (await screen.findByRole("textbox", { name: "等待其他会话释放写入权的最长时间" })) as HTMLInputElement;
    expect(box.value).toBe("0");
    await userEvent.clear(box);
    await userEvent.type(box, "30");
    await userEvent.tab();
    expect(save).toHaveBeenCalledWith({ maxWaitMinutes: 30 });
    await waitFor(() => expect(box.value).toBe("30"));
    expect(screen.getByText(/workspace\.claim_timeout/)).toBeTruthy();
  });

  it("keeps the switch where it was when the save is refused", async () => {
    const p = new MockPort() as unknown as AgentPort;
    p.saveWriteLease = async () => {
      throw new Error("disk full");
    };
    render(<WriteLease port={p} />);
    const sw = await toggle();
    await userEvent.click(sw);
    await screen.findByText(/disk full/);
    expect(sw.getAttribute("aria-checked")).toBe("true");
  });

  it("says so when the setting cannot be read", async () => {
    const p = new MockPort() as unknown as AgentPort;
    p.writeLease = async () => {
      throw new Error("offline");
    };
    render(<WriteLease port={p} />);
    expect(await screen.findByText("无法读取多会话写入设置。")).toBeTruthy();
  });
});
