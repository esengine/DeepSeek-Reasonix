// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Packages } from "./Packages";
import { MockPort } from "../port/mock";
import type { AgentPort, PluginExport } from "../port/port";
import { boot, STORAGE, t } from "../i18n";

afterEach(() => {
  cleanup();
  localStorage.setItem(STORAGE, "zh");
  boot();
  vi.restoreAllMocks();
});

const previous: PluginExport = { savedTo: "/tmp/previous-export.zip", required: ["PREVIOUS_TOKEN"] };

async function setup(lang = "zh") {
  localStorage.setItem(STORAGE, lang);
  boot();
  const port = new MockPort() as unknown as AgentPort;
  const packages = (await port.plugins()).filter((p) => p.name === "review-kit");
  const onChanged = vi.fn();
  const exportPlugin = vi.spyOn(port, "exportPlugin");
  const view = render(<Packages port={port} packages={packages} onChanged={onChanged} updating="" onUpdate={() => {}} />);
  const details = view.container.querySelector<HTMLDetailsElement>("details")!;
  return { port, packages, onChanged, exportPlugin, view, details, row: within(details) };
}

describe("package export feedback", () => {
  it.each([
    ["zh", "saved"], ["en", "saved"], ["zh", "downloaded"], ["en", "downloaded"],
  ])("shows the %s %s outcome and recipient requirements from a collapsed row", async (lang, destination) => {
    const { row, details, exportPlugin } = await setup(lang);
    const result: PluginExport = { required: ["REVIEW_TOKEN", "NOTES_TOKEN"], ...(destination === "saved" ? { savedTo: "/tmp/review-kit.zip" } : {}) };
    exportPlugin.mockResolvedValueOnce(result);
    expect(details.open).toBe(false);
    const user = userEvent.setup();
    const button = row.getByRole("button", { name: t("导出") });
    button.focus();
    await user.keyboard("{Enter}");
    await row.findByText(/REVIEW_TOKEN/);
    expect(details.open).toBe(true);
    const status = row.getByRole("status");
    expect(status.textContent).toContain(destination === "saved" ? t("已保存至 {path}。", { path: result.savedTo! }) : t("导出完成。"));
    expect(status.textContent).toContain(t("里面的密钥值已经去掉，装它的人要自己提供：{names}", { names: "REVIEW_TOKEN、NOTES_TOKEN" }));
    if (lang === "en") expect(status.textContent).toBe(`${destination === "saved" ? "Saved to /tmp/review-kit.zip." : "Exported."} The key values were stripped out; whoever installs it supplies their own: REVIEW_TOKEN、NOTES_TOKEN`);
    expect(exportPlugin).toHaveBeenCalledExactlyOnceWith("review-kit");
    expect(row.getByRole<HTMLButtonElement>("button", { name: t("导出") }).disabled).toBe(false);
  });

  it.each(["success", "failure"])("retires a previous result while a retry is pending and after %s", async (outcome) => {
    const { row, exportPlugin, onChanged } = await setup();
    let finish!: (result: PluginExport) => void;
    let fail!: (error: Error) => void;
    exportPlugin.mockResolvedValueOnce(previous).mockImplementationOnce(() => new Promise<PluginExport>((resolve, reject) => { finish = resolve; fail = reject; }));
    await userEvent.click(row.getByRole("button", { name: "导出" }));
    await row.findByText(/PREVIOUS_TOKEN/);
    await userEvent.click(row.getByRole("button", { name: "导出" }));
    expect(row.queryByText(/previous-export.zip|PREVIOUS_TOKEN/)).toBeNull();
    const pending = row.getByRole<HTMLButtonElement>("button", { name: "打包中…" });
    expect(pending.disabled).toBe(true);
    await userEvent.click(pending);
    expect(exportPlugin).toHaveBeenCalledTimes(2);
    await act(async () => outcome === "success" ? finish({ required: [] }) : fail(new Error("archive could not be saved")));
    expect(row.queryByText(/previous-export.zip|PREVIOUS_TOKEN/)).toBeNull();
    if (outcome === "success") expect(row.getByRole("status").textContent).toContain("该包不需要填写任何密钥。");
    else {
      expect(row.getByText("archive could not be saved")).toBeTruthy();
      expect(row.queryByRole("status")).toBeNull();
      expect(row.queryByText(/导出完成。|已保存至/)).toBeNull();
      exportPlugin.mockResolvedValueOnce({ savedTo: "/tmp/retried.zip", required: ["CURRENT_TOKEN"] });
      await userEvent.click(row.getByRole("button", { name: "导出" }));
      await row.findByText(/CURRENT_TOKEN/);
      expect(row.getByRole("status").textContent).toContain("/tmp/retried.zip");
      expect(row.queryByText("archive could not be saved")).toBeNull();
    }
    expect(row.getByRole<HTMLButtonElement>("button", { name: "导出" }).disabled).toBe(false);
    expect(onChanged).toHaveBeenCalledTimes(outcome === "success" ? 2 : 3);
  });

  it("separates the English outcome from the no-key requirement", async () => {
    const { row, exportPlugin } = await setup("en");
    exportPlugin.mockResolvedValueOnce({ required: [] });
    await userEvent.click(row.getByRole("button", { name: "Export" }));
    expect((await row.findByRole("status")).textContent).toBe("Exported. This package asks for no keys.");
  });

  it("does not present an earlier successful archive beside a new export failure", async () => {
    const { row, exportPlugin } = await setup();
    exportPlugin.mockResolvedValueOnce(previous).mockRejectedValueOnce(new Error("archive could not be saved"));
    await userEvent.click(row.getByRole("button", { name: "导出" }));
    await row.findByText(/PREVIOUS_TOKEN/);
    await userEvent.click(row.getByRole("button", { name: "导出" }));
    await row.findByText("archive could not be saved");
    expect(row.queryByText(/previous-export.zip|PREVIOUS_TOKEN/)).toBeNull();
    expect(row.queryByRole("status")).toBeNull();
  });

  it("keeps a saved result across inventory refresh and unrelated activation changes", async () => {
    const { port, row, exportPlugin, packages, onChanged, view } = await setup();
    exportPlugin.mockResolvedValueOnce(previous);
    await userEvent.click(row.getByRole("button", { name: "导出" }));
    await row.findByText(/PREVIOUS_TOKEN/);
    view.rerender(<Packages port={port} packages={packages.map((p) => ({ ...p }))} onChanged={onChanged} updating="" onUpdate={() => {}} />);
    await userEvent.click(row.getByRole("switch", { name: "关闭 review-kit" }));
    expect(row.getByText(/previous-export.zip/)).toBeTruthy();
    expect(exportPlugin).toHaveBeenCalledTimes(1);
    expect(onChanged).toHaveBeenCalledTimes(2);
  });
});
