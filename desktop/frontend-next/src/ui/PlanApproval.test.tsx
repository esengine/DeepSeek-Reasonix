// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { PlanApproval } from "./PlanApproval";
import { MockPort } from "../port/mock";
import type { AgentPort } from "../port/port";

afterEach(cleanup);

const every = () => screen.findByRole("radio", { name: "每次" });
const never = () => screen.findByRole("radio", { name: "不需要" });

describe("the plan-approval switch", () => {
  it("starts off, says what it releases and what it leaves alone, and saves the opposite of what it shows", async () => {
    const p = new MockPort() as unknown as AgentPort;
    const save = vi.spyOn(p, "savePlanApproval");
    render(<PlanApproval port={p} />);
    const ask = await every();
    expect(ask.getAttribute("aria-checked")).toBe("true");
    const why = screen.getByText("计划做好后要不要先给你看").getAttribute("title") ?? "";
    expect(why).toMatch(/工具批准、沙箱和拒绝规则照常生效/);
    expect(why).toMatch(/项目配置不能替你打开/);
    await userEvent.click(await never());
    expect(save).toHaveBeenCalledWith(true);
    await waitFor(() => expect((screen.getByRole("radio", { name: "不需要" })).getAttribute("aria-checked")).toBe("true"));
    await userEvent.click(screen.getByRole("radio", { name: "每次" }));
    expect(save).toHaveBeenLastCalledWith(false);
  });

  it("keeps the switch where it was when the save is refused", async () => {
    const p = new MockPort() as unknown as AgentPort;
    p.savePlanApproval = async () => {
      throw new Error("disk full");
    };
    render(<PlanApproval port={p} />);
    await userEvent.click(await never());
    await screen.findByText(/disk full/);
    expect((await every()).getAttribute("aria-checked")).toBe("true");
  });

  it("says so when the setting cannot be read", async () => {
    const p = new MockPort() as unknown as AgentPort;
    p.planApproval = async () => {
      throw new Error("offline");
    };
    render(<PlanApproval port={p} />);
    expect(await screen.findByText("无法读取计划批准设置。")).toBeTruthy();
  });
});
