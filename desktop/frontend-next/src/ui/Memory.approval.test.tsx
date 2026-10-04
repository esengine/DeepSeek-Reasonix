// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { RememberApproval } from "./Memory";
import { MockPort } from "../port/mock";
import type { AgentPort, RememberApprovalSettings } from "../port/port";

afterEach(cleanup);

const base: RememberApprovalSettings = {
  projectAutoConfirm: false, projectEffective: false,
  path: "~/.reasonix/config.toml",
};

const port = (over: Partial<RememberApprovalSettings> = {}) => {
  const p = new MockPort() as unknown as AgentPort;
  p.rememberApproval = async () => ({ ...base, ...over });
  return p;
};

const projectSwitch = () => screen.findByRole("switch", { name: "项目记忆写入免确认" });

describe("the remember-confirmation switch", () => {
  it("saves the answer, which covers a project write only", async () => {
    const p = port();
    const save = vi.fn(async (s: { projectAutoConfirm: boolean }) => ({
      ...base, ...s, projectEffective: s.projectAutoConfirm,
    }));
    p.saveRememberApproval = save;
    const changed = vi.fn();
    render(<RememberApproval port={p} onChanged={changed} />);
    const sw = await projectSwitch();
    expect(sw.getAttribute("aria-checked")).toBe("false");
    await userEvent.click(sw);
    expect(save).toHaveBeenCalledWith({ projectAutoConfirm: true });
    await waitFor(() => expect(sw.getAttribute("aria-checked")).toBe("true"));
    expect(changed).toHaveBeenCalledTimes(1);
  });

  // A global fact reaches every project, so it keeps asking whatever this says.
  it("offers one switch, not one per scope", async () => {
    render(<RememberApproval port={port()} onChanged={() => {}} />);
    await projectSwitch();
    expect(screen.queryAllByRole("switch")).toHaveLength(1);
  });

  it("keeps the switch where it was when the save is refused", async () => {
    const p = port();
    p.saveRememberApproval = async () => {
      throw new Error("disk full");
    };
    const changed = vi.fn();
    render(<RememberApproval port={p} onChanged={changed} />);
    const sw = await projectSwitch();
    await userEvent.click(sw);
    await screen.findByText(/disk full/);
    expect(sw.getAttribute("aria-checked")).toBe("false");
    expect(changed).not.toHaveBeenCalled();
  });

  it("says the setting could not be read rather than drawing a switch with no value", async () => {
    const p = new MockPort() as unknown as AgentPort;
    p.rememberApproval = async () => {
      throw new Error("no kernel");
    };
    render(<RememberApproval port={p} onChanged={() => {}} />);
    await screen.findByText("无法读取记忆写入设置。");
  });
});
