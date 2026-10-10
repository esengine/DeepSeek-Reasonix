// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Packages } from "./Packages";
import { MockPort } from "../port/mock";
import type { AgentPort, PluginExport, PluginPackage } from "../port/port";
import { boot, STORAGE, t } from "../i18n";

afterEach(() => {
  cleanup();
  localStorage.setItem(STORAGE, "zh");
  boot();
  vi.restoreAllMocks();
});

function draw(port: AgentPort, packages: PluginPackage[], onChanged: () => void) {
  return <Packages port={port} packages={packages} onChanged={onChanged} updating="" onUpdate={() => {}} />;
}

async function setup(lang = "zh") {
  localStorage.setItem(STORAGE, lang);
  boot();
  const user = userEvent.setup();
  const write = vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue(undefined);
  const port = new MockPort() as unknown as AgentPort;
  const packages = (await port.plugins()).filter((p) => p.name === "review-kit");
  const onChanged = vi.fn();
  const exportPlugin = vi.spyOn(port, "exportPlugin");
  const view = render(draw(port, packages, onChanged));
  const row = within(view.container.querySelector('[data-extension-name="review-kit"]')! as HTMLElement);
  return { user, write, port, packages, onChanged, exportPlugin, view, row };
}

describe("copying an exported package's saved path", () => {
  it.each([
    ["zh", "/Users/me/交付资料/review kit.zip"],
    ["en", "C:\\Users\\me\\Release notes\\review-kit.zip"],
  ])("copies the literal native save result with the %s keyboard action", async (lang, savedTo) => {
    const { user, row, write, exportPlugin, onChanged } = await setup(lang);
    exportPlugin.mockResolvedValueOnce({ savedTo, required: ["REVIEW_TOKEN"] });
    await user.click(row.getByRole("button", { name: t("导出") }));
    const copy = await row.findByRole("button", { name: t("复制路径") });
    expect(copy.textContent).toBe(t("复制路径"));
    expect(copy.getAttribute("title")).toBe(t("复制路径"));
    copy.focus();
    await user.keyboard("{Enter}");
    expect(write).toHaveBeenCalledExactlyOnceWith(savedTo);
    expect(document.activeElement).toBe(copy);
    expect(copy.textContent).toBe(t("已复制"));
    expect(row.getByRole("status").textContent).toContain(savedTo);
    expect(row.getByRole("status").textContent).toContain("REVIEW_TOKEN");
    expect(exportPlugin).toHaveBeenCalledExactlyOnceWith("review-kit");
    expect(onChanged).toHaveBeenCalledTimes(1);
  });

  it.each([undefined, ""])("does not infer a native path from a browser export answer %j", async (savedTo) => {
    const { user, row, exportPlugin, write } = await setup();
    expect(row.queryByRole("button", { name: "复制路径" })).toBeNull();
    exportPlugin.mockResolvedValueOnce({ savedTo, required: [] });
    await user.click(row.getByRole("button", { name: "导出" }));
    await row.findByRole("status");
    expect(row.queryByRole("button", { name: "复制路径" })).toBeNull();
    expect(row.getByRole("status").textContent).toContain("导出完成。");
    expect(write).not.toHaveBeenCalled();
  });

  it("retries a refused clipboard write without exporting the package again", async () => {
    const { user, row, exportPlugin, write } = await setup();
    exportPlugin.mockResolvedValueOnce({ savedTo: "/exports/review.zip", required: [] });
    write.mockRejectedValueOnce(new Error("clipboard unavailable"));
    await user.click(row.getByRole("button", { name: "导出" }));
    const copy = await row.findByRole("button", { name: "复制路径" });
    await user.click(copy);
    expect(copy.textContent).toBe("复制失败，请重试");
    expect(row.getByRole("status").textContent).toContain("/exports/review.zip");
    await user.click(copy);
    expect(copy.textContent).toBe("已复制");
    expect(write).toHaveBeenNthCalledWith(2, "/exports/review.zip");
    expect(exportPlugin).toHaveBeenCalledTimes(1);
  });

  it.each(["success", "failure"])("retires the previous copy action while a new export is pending and after %s", async (outcome) => {
    const { user, row, exportPlugin, write } = await setup();
    let finish!: (result: PluginExport) => void;
    let fail!: (error: Error) => void;
    exportPlugin.mockResolvedValueOnce({ savedTo: "/exports/old.zip", required: [] })
      .mockImplementationOnce(() => new Promise<PluginExport>((resolve, reject) => { finish = resolve; fail = reject; }));
    await user.click(row.getByRole("button", { name: "导出" }));
    await row.findByRole("button", { name: "复制路径" });
    await user.click(row.getByRole("button", { name: "导出" }));
    expect(row.queryByRole("button", { name: "复制路径" })).toBeNull();
    await act(async () => outcome === "success" ? finish({ savedTo: "/exports/new.zip", required: [] }) : fail(new Error("save failed")));
    if (outcome === "success") {
      await user.click(row.getByRole("button", { name: "复制路径" }));
      expect(write).toHaveBeenCalledExactlyOnceWith("/exports/new.zip");
    } else {
      expect(row.queryByRole("button", { name: "复制路径" })).toBeNull();
      expect(row.getByRole("alert").textContent).toBe("save failed");
      expect(write).not.toHaveBeenCalled();
    }
  });

  it("preserves the copy result across an inventory refresh and activation", async () => {
    const { user, row, port, view, packages, exportPlugin, write, onChanged } = await setup();
    exportPlugin.mockResolvedValueOnce({ savedTo: "/exports/review.zip", required: [] });
    await user.click(row.getByRole("button", { name: "导出" }));
    const copy = await row.findByRole("button", { name: "复制路径" });
    await user.click(copy);
    view.rerender(draw(port, packages.map((p) => ({ ...p })), onChanged));
    expect(row.getByRole("button", { name: "复制路径" })).toBe(copy);
    expect(copy.textContent).toBe("已复制");
    await user.click(row.getByRole("switch", { name: "关闭 review-kit" }));
    expect(row.getByRole("button", { name: "复制路径" })).toBe(copy);
    expect(write).toHaveBeenCalledTimes(1);
    expect(exportPlugin).toHaveBeenCalledTimes(1);
  });

  it.each([false, true])("isolates the path after replacing a connection (return=%s)", async (returnToPort) => {
    const { user, row, port, view, packages, exportPlugin, write, onChanged } = await setup();
    const next = new MockPort() as unknown as AgentPort;
    exportPlugin.mockResolvedValueOnce({ savedTo: "/exports/previous.zip", required: [] });
    await user.click(row.getByRole("button", { name: "导出" }));
    await row.findByRole("button", { name: "复制路径" });
    view.rerender(draw(next, packages, onChanged));
    if (returnToPort) view.rerender(draw(port, packages, onChanged));
    expect(screen.queryByRole("button", { name: "复制路径" })).toBeNull();
    expect(screen.queryByText(/previous.zip/)).toBeNull();
    const currentExport = returnToPort ? exportPlugin : vi.spyOn(next, "exportPlugin");
    currentExport.mockResolvedValueOnce({ savedTo: "/exports/current.zip", required: [] });
    await user.click(screen.getByRole("button", { name: "导出" }));
    await user.click(await screen.findByRole("button", { name: "复制路径" }));
    expect(write).toHaveBeenCalledExactlyOnceWith("/exports/current.zip");
  });
});
