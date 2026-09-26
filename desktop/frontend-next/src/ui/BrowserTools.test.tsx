// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { BrowserTools } from "./BrowserTools";
import { MockPort } from "../port/mock";
import type { AgentPort, BrowserToolsSettings } from "../port/port";

afterEach(cleanup);

const port = (over: Partial<BrowserToolsSettings> = {}) => {
  const p = new MockPort() as unknown as AgentPort;
  const base = { enabled: true, effective: true, path: "~/.reasonix/config.toml", ...over };
  p.browserTools = async () => ({ ...base });
  return p;
};

const toggle = () => screen.findByRole("switch", { name: "启用内置浏览器" });

describe("the built-in browser switch", () => {
  it("saves the opposite of what it shows and draws what came back", async () => {
    const p = port();
    const save = vi.fn(async (enabled: boolean) => ({ enabled, effective: enabled, path: "~/.reasonix/config.toml" }));
    p.saveBrowserTools = save;
    const changed = vi.fn();
    render(<BrowserTools port={p} onChanged={changed} />);
    const sw = await toggle();
    expect(sw.getAttribute("aria-checked")).toBe("true");
    await userEvent.click(sw);
    expect(save).toHaveBeenCalledWith(false);
    await waitFor(() => expect(sw.getAttribute("aria-checked")).toBe("false"));
    expect(changed).toHaveBeenCalledTimes(1);
  });

  it("keeps the switch where it was when the save is refused", async () => {
    const p = port();
    p.saveBrowserTools = async () => {
      throw new Error("disk full");
    };
    const changed = vi.fn();
    render(<BrowserTools port={p} onChanged={changed} />);
    const sw = await toggle();
    await userEvent.click(sw);
    await screen.findByText(/disk full/);
    expect(sw.getAttribute("aria-checked")).toBe("true");
    expect(changed).not.toHaveBeenCalled();
  });

  it("says what this workspace runs with when a project file outranks the switch", async () => {
    render(<BrowserTools port={port({ enabled: true, effective: false })} onChanged={() => {}} />);
    await toggle();
    expect(screen.getByText("当前项目的配置文件关闭了它，此工作区不会提供内置浏览器。")).toBeTruthy();
  });

  it("adds nothing when the switch is what runs", async () => {
    render(<BrowserTools port={port()} onChanged={() => {}} />);
    await toggle();
    expect(screen.queryByText("实际生效")).toBeNull();
  });
});
