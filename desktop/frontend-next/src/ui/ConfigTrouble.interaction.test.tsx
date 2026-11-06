// @vitest-environment jsdom
import "./testkit";
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ConfigTrouble } from "./ConfigTrouble";
import type { AgentPort, ConfigProblem } from "../port/port";

afterEach(cleanup);

const problem = (over: Partial<ConfigProblem> = {}): ConfigProblem => ({
  path: "~/.reasonix/config.toml", line: 12, excerpt: "model = deepseek-v4-pro", recovered: "last-known-good", ...over,
}) as ConfigProblem;

function draw(p: ConfigProblem | null, repair = vi.fn(async () => ({ problem: null }))) {
  const port = { configProblem: async () => p, repairConfig: repair } as unknown as AgentPort;
  const onRepaired = vi.fn();
  const view = render(<ConfigTrouble port={port} onRepaired={onRepaired} />);
  return { view, repair, onRepaired };
}

describe("the config banner at the top of settings", () => {
  it("says nothing when the file parses", async () => {
    const { view } = draw(null);
    await Promise.resolve();
    expect(view.container.querySelector(".cfgbad")).toBeNull();
  });

  it("names the line, what is shown instead, the path and the offending text", async () => {
    draw(problem());
    const bar = await waitFor(() => { const e = document.querySelector(".cfgbad"); expect(e).toBeTruthy(); return e!; });
    expect(bar.querySelector(".cfg-hd b")?.textContent).toBe("配置文件第 12 行无法解析");
    expect(bar.querySelector(".cfg-hd .cfg-sub")?.textContent).toContain("上一次成功解析的配置");
    expect(bar.querySelector(".cfg-path span")?.textContent).toBe("~/.reasonix/config.toml");
    expect(bar.querySelector(".cfg-diff .er")?.textContent).toBe("12model = deepseek-v4-pro");
    expect(bar.querySelector(".cfg-card")?.getAttribute("data-lvl")).toBe("warn");
    expect(bar.querySelector(".cfg-ft")).toBeNull();
  });

  it("offers the repair as the main action only when the file parses afterwards", async () => {
    const { repair, onRepaired } = draw(problem({ repair: 'model = "deepseek-v4-pro"' }));
    const button = await screen.findByRole("button", { name: "备份原文件并修复" });
    expect(button.hasAttribute("data-primary")).toBe(true);
    expect(document.querySelector(".cfg-card")?.getAttribute("data-lvl")).toBe("err");
    expect(document.querySelector(".cfg-diff .ok")?.textContent).toBe('改成model = "deepseek-v4-pro"');
    await userEvent.click(button);
    expect(repair).toHaveBeenCalledOnce();
    await waitFor(() => expect(onRepaired).toHaveBeenCalled());
  });

  it("shows the refusal when the repair fails", async () => {
    const repair = vi.fn(async () => { throw new Error("disk is read-only"); });
    draw(problem({ repair: 'model = "x"' }), repair as never);
    await userEvent.click(await screen.findByRole("button", { name: "备份原文件并修复" }));
    expect((await screen.findByRole("alert")).textContent).toContain("disk is read-only");
  });
});
