// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Settings } from "./Settings";
import { AddPlugin } from "./AddPlugin";
import type { AgentPort, PluginPlan, SessionStatus } from "../port/port";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

function draw(at?: string, port = new MockPort() as unknown as AgentPort) {
  const onClose = vi.fn();
  render(
    <Settings
      hub={new MockHub() as never}
      port={port}
      status={{ preset: "balanced", toolApprovalMode: "ask" } as SessionStatus}
      theme="light" onTheme={() => {}} contrast="" onContrast={() => {}} weight="" onWeight={() => {}}
      look={{} as never} onLook={() => {}} reloadThemes={() => {}}
      onClose={onClose} onChanged={() => {}} onError={() => {}}
      at={at}
      account={null} accountUnread="" reloadAccount={() => {}}
    />,
  );
  return { onClose };
}

it("opens the installed tab after a market install and keeps keyboard focus there", async () => {
  const port = new MockPort() as unknown as AgentPort;
  const install = port.installMarket.bind(port);
  port.installMarket = async (req) => {
    const out = await install(req);
    return { ...out, actions: [{ kind: "plugin", action: "install_plugin_package", status: "done", riskLevel: "high", name: "manifest-kit" }, ...(out.actions ?? [])] };
  };
  const packages = port.plugins.bind(port);
  port.plugins = async () => (await packages()).map((p) => p.name === "review-kit" ? { ...p, name: "manifest-kit" } : p);
  draw("ext:market", port);
  await userEvent.click(await screen.findByRole("button", { name: /review-kit/ }));
  await userEvent.click(await screen.findByRole("button", { name: "查看将安装的内容" }));
  await userEvent.click(screen.getByRole("checkbox", { name: "我已看过这 3 个技能，全部安装" }));
  await userEvent.click(screen.getByRole("button", { name: "安装" }));
  await userEvent.click(await screen.findByRole("button", { name: "查看已安装能力" }));
  const installed = screen.getByRole("tab", { name: "已安装" });
  await waitFor(() => expect(installed.getAttribute("aria-selected")).toBe("true"));
  await waitFor(() => expect(document.activeElement?.closest("[data-extension-name='manifest-kit']")).toBeTruthy());
});

describe("a package's inline update", () => {
  it("keeps focus where the user moved after a failed update when the inventory refreshes", async () => {
    const port = new MockPort() as unknown as AgentPort;
    const updating = (await port.plugins())[0];
    vi.spyOn(port, "installPlugin").mockRejectedValueOnce(new Error("update unavailable"));
    const drawUpdate = (p: typeof updating) => <>
      <button>Other control</button>
      <AddPlugin port={port} updating={p} onClose={() => {}} onInstalled={() => {}} />
    </>;
    const { rerender } = render(drawUpdate(updating));
    await userEvent.click(await screen.findByRole("button", { name: "更新" }));
    await screen.findByRole("alert");
    await userEvent.click(screen.getByRole("button", { name: "Other control" }));
    rerender(drawUpdate({ ...updating }));
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "Other control" }));
  });

  it("locks the selected row until its update preview is dismissed", async () => {
    const port = new MockPort() as unknown as AgentPort;
    let finish!: (value: PluginPlan) => void;
    const preview = vi.spyOn(port, "planPlugin").mockImplementationOnce(() => new Promise((resolve) => { finish = resolve; }));
    const toggle = vi.spyOn(port, "setPluginEnabled");
    const remove = vi.spyOn(port, "removePlugin");
    const exportPackage = vi.spyOn(port, "exportPlugin");
    draw("ext:installed", port);
    const row = (await screen.findByText("review-kit")).closest("details")!;
    const controls = within(row);
    await userEvent.click(controls.getByRole("button", { name: "移除 review-kit" }));
    await userEvent.click(controls.getByRole("button", { name: "更新" }));
    await waitFor(() => expect(preview).toHaveBeenCalledTimes(1));
    expect(row.getAttribute("aria-busy")).toBe("true");
    const add = screen.getByRole<HTMLButtonElement>("button", { name: "添加" });
    expect(add.disabled).toBe(true);
    await userEvent.click(add);
    expect(document.querySelectorAll(".addpkg")).toHaveLength(1);

    expect(controls.getByRole<HTMLButtonElement>("switch", { name: "关闭 review-kit" }).disabled).toBe(true);
    for (const name of ["导出", "移除 review-kit", "删除", "取消"]) {
      const button = controls.getByRole<HTMLButtonElement>("button", { name });
      expect(button.disabled).toBe(true);
      await userEvent.click(button);
    }
    await userEvent.click(controls.getByRole("switch", { name: "关闭 review-kit" }));
    expect(toggle).not.toHaveBeenCalled();
    expect(remove).not.toHaveBeenCalled();
    expect(exportPackage).not.toHaveBeenCalled();
    expect(screen.getByRole<HTMLButtonElement>("switch", { name: "启用 notion-bridge" }).disabled).toBe(false);
    const otherUpdate = within(document.querySelector<HTMLElement>('[data-extension-name="notion-bridge"]')!).getByRole<HTMLButtonElement>("button", { name: "更新" });
    expect(otherUpdate.disabled).toBe(true);
    await userEvent.click(otherUpdate);
    expect(preview).toHaveBeenCalledTimes(1);
    const panel = document.querySelector<HTMLElement>('.addpkg[data-stage="reading"]')!;
    expect(panel.getAttribute("aria-busy")).toBe("true");
    await userEvent.click(within(panel).getByRole("button", { name: "取消" }));
    expect(document.querySelector(".addpkg")).toBeNull();
    expect(controls.getByRole<HTMLButtonElement>("switch", { name: "关闭 review-kit" }).disabled).toBe(false);
    expect(controls.getByRole<HTMLButtonElement>("button", { name: "删除" }).disabled).toBe(false);
    expect(otherUpdate.disabled).toBe(false);
    expect(row.getAttribute("aria-busy")).toBe("false");
    expect(add.disabled).toBe(false);
    await act(async () => finish({ ok: true, applied: false, status: "planned", actions: [] }));
  });

  it.each(["success", "failure", "refused"])("keeps an applying update mounted through Cancel and Escape, then permits dismissal after %s", async (outcome) => {
    const port = new MockPort() as unknown as AgentPort;
    const listeners = vi.spyOn(window, "addEventListener");
    let finish!: (value: PluginPlan) => void;
    let fail!: (error: Error) => void;
    const install = vi.spyOn(port, "installPlugin").mockImplementationOnce(() => new Promise((resolve, reject) => { finish = resolve; fail = reject; }));
    const { onClose } = draw("ext:installed", port);
    const row = (await screen.findByText("review-kit")).closest("details")!;
    await userEvent.click(within(row).getByRole("button", { name: "更新" }));
    await screen.findByText(/review-kit.*→/);
    const panel = document.querySelector<HTMLElement>('.addpkg[data-stage="confirm"]')!;
    const apply = within(panel).getByRole<HTMLButtonElement>("button", { name: "更新" });
    apply.focus();
    await userEvent.keyboard("{Enter}");
    expect(install).toHaveBeenCalledTimes(1);

    const cancel = within(panel).getByRole<HTMLButtonElement>("button", { name: "取消" });
    expect(cancel.disabled).toBe(true);
    expect(panel.getAttribute("aria-busy")).toBe("true");
    const close = document.querySelector<HTMLButtonElement>('[data-action="settings.close"]')!;
    expect(close.disabled).toBe(true);
    const section = document.querySelector<HTMLButtonElement>('[data-action="settings.section"][data-value="session"]')!;
    const market = screen.getByRole<HTMLButtonElement>("tab", { name: "发现" });
    expect(section.disabled).toBe(true);
    expect(market.disabled).toBe(true);
    await userEvent.click(section);
    await userEvent.click(market);
    await userEvent.click(close);
    await userEvent.click(document.querySelector<HTMLElement>(".prefs")!);
    await userEvent.click(cancel);
    await userEvent.keyboard("{Escape}");
    // The form consumes Escape in capture; verify the sheet's separate listener too.
    const escape = [...listeners.mock.calls].reverse().find(([type, , options]) => type === "keydown" && !options)?.[1];
    expect(typeof escape).toBe("function");
    act(() => (escape as EventListener).call(window, new KeyboardEvent("keydown", { key: "Escape" })));
    expect(onClose).not.toHaveBeenCalled();
    expect(document.querySelector('.addpkg[data-stage="confirm"]')).toBe(panel);
    expect(within(row).getByRole<HTMLButtonElement>("switch", { name: "关闭 review-kit" }).disabled).toBe(true);

    let dismiss: HTMLElement;
    if (outcome !== "failure") {
      await act(async () => finish(outcome === "success"
        ? { ok: true, applied: true, status: "done", actions: [{ kind: "plugin", action: "install_plugin_package", status: "done", name: "review-kit", riskLevel: "low" }] }
        : { ok: false, applied: false, status: "failed", error: "update refused", actions: [] }));
      const done = await screen.findByRole("button", { name: "完成" });
      const next = outcome === "success" ? done : screen.getByRole("button", { name: "重试" });
      await waitFor(() => expect(document.activeElement).toBe(next));
      dismiss = done;
    } else {
      await act(async () => fail(new Error("update unavailable")));
      expect(await within(panel).findByText("update unavailable")).toBeTruthy();
      expect(cancel.disabled).toBe(false);
      expect(panel.getAttribute("aria-busy")).toBe("false");
      await waitFor(() => expect(document.activeElement).toBe(apply));
      dismiss = cancel;
    }
    expect(document.querySelector(".addpkg")).toBeTruthy();
    expect(close.disabled).toBe(false);
    expect(section.disabled).toBe(false);
    expect(market.disabled).toBe(false);
    expect(within(row).getByRole<HTMLButtonElement>("switch", { name: "关闭 review-kit" }).disabled).toBe(true);
    expect(screen.getByRole<HTMLButtonElement>("button", { name: "添加" }).disabled).toBe(true);
    await userEvent.click(dismiss);
    expect(document.querySelector(".addpkg")).toBeNull();
    expect(within(row).getByRole<HTMLButtonElement>("switch", { name: "关闭 review-kit" }).disabled).toBe(false);
    expect(close.disabled).toBe(false);
    expect(section.disabled).toBe(false);
    expect(market.disabled).toBe(false);
  });
});

describe("what Escape takes back", () => {
  // The sheet listens for Escape on the window. An inline form that does not
  // claim the key first goes down with the whole sheet — and takes what was
  // typed into it, which is the one thing pressing Escape there cannot mean.
  it("closes the form it was pressed in, not the sheet around it", async () => {
    const { onClose } = draw();
    await userEvent.click(screen.getByRole("tab", { name: /扩展/ }));
    const add = await screen.findByRole("button", { name: "添加" });
    await userEvent.click(add);
    await screen.findByRole("textbox", { name: /粘贴|安装/ }).catch(() => null);

    await userEvent.keyboard("{Escape}");
    expect(onClose).not.toHaveBeenCalled();
    await waitFor(() => expect(screen.queryByRole("button", { name: "添加" })).not.toBeNull());

    // With nothing open above it, the same key does close the sheet.
    await userEvent.keyboard("{Escape}");
    expect(onClose).toHaveBeenCalled();
  });
});

describe("the section list as one tab stop", () => {
  // role=tablist means arrows move inside the list and Tab steps over it.
  // Twelve stops made Tab the wrong way to get anywhere on this screen.
  it("keeps every unselected section out of the tab order", async () => {
    draw();
    const tabs = screen.getAllByRole("tab");
    expect(tabs.length).toBeGreaterThan(6);
    const stops = tabs.filter((t) => t.tabIndex === 0);
    expect(stops).toHaveLength(1);
    expect(stops[0].getAttribute("aria-selected")).toBe("true");
  });
});

describe("where focus is when the sheet goes", () => {
  // Closing used to leave focus on <body>: a reader on the keyboard came out
  // of settings with no position and had to tab in from the top of the window.
  it("puts focus back on whatever opened it", async () => {
    const opener = document.createElement("button");
    document.body.append(opener);
    opener.focus();
    const { unmount } = render(
      <Settings
        hub={new MockHub() as never}
        port={new MockPort() as unknown as AgentPort}
        status={{ preset: "balanced", toolApprovalMode: "ask" } as SessionStatus}
        theme="light" onTheme={() => {}} contrast="" onContrast={() => {}} weight="" onWeight={() => {}}
        look={{} as never} onLook={() => {}} reloadThemes={() => {}}
        onClose={() => {}} onChanged={() => {}} onError={() => {}}
        account={null} accountUnread="" reloadAccount={() => {}}
      />,
    );
    expect(document.activeElement).not.toBe(opener);
    unmount();
    expect(document.activeElement).toBe(opener);
    opener.remove();
  });
});
