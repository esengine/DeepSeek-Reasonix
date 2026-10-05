// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Policy } from "./Policy";
import { MockPort } from "../port/mock";
import type { AgentPort, ApprovalMode, SessionStatus } from "../port/port";

afterEach(cleanup);

const status = (mode: ApprovalMode = "ask") =>
  ({ preset: "balanced", effort: "auto", toolApprovalMode: mode } as SessionStatus);

function draw(mode: ApprovalMode = "ask", onBoundary?: () => void) {
  const onChanged = vi.fn();
  const port = new MockPort() as unknown as AgentPort;
  const view = render(<Policy port={port} status={status(mode)} onChanged={onChanged} onBoundary={onBoundary} />);
  return { ...view, port, onChanged };
}

describe("execution permission control", () => {
  it("names the committed permission directly", () => {
    const { container } = draw();
    expect(container.querySelector('[data-action="chrome.policy"]')?.getAttribute("aria-label")).toBe("执行权限：询问");
    expect(container.querySelector(".policy")?.hasAttribute("data-quiet")).toBe(true);
  });

  it("keeps the dangerous state visible on the closed trigger", () => {
    const { container } = draw("yolo");
    expect(container.querySelector(".polrisk")?.textContent).toContain("全部放行");
    expect(container.querySelector(".polwarn svg")).not.toBeNull();
  });

  it("contains permission choices only", async () => {
    draw();
    await userEvent.click(screen.getByRole("button", { name: "执行权限：询问" }));
    const group = screen.getByRole("group", { name: "执行权限" });
    expect(group.textContent).toMatch(/不询问.*询问.*自动批准.*全部放行/s);
    expect(group.textContent).not.toMatch(/高级执行设置|均衡|交付|思考强度/);
  });

  it("opens the sandbox boundary from the same layer", async () => {
    const onBoundary = vi.fn();
    draw("ask", onBoundary);
    await userEvent.click(screen.getByRole("button", { name: "执行权限：询问" }));
    await userEvent.click(screen.getByRole("button", { name: /沙盒与运行边界/ }));
    expect(onBoundary).toHaveBeenCalledTimes(1);
  });

  it("draws nothing before the kernel has answered", () => {
    const { container } = render(
      <Policy port={new MockPort() as unknown as AgentPort} status={null} onChanged={() => {}} />,
    );
    expect(container.querySelector(".policy")).toBeNull();
  });
});
