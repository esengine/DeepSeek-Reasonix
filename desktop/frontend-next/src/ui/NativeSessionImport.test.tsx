// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { MockHub } from "../port/mock_hub";
import { NativeSessionImport } from "./NativeSessionImport";
import { HttpError } from "../port/http_error";
import { boot, STORAGE } from "../i18n";

afterEach(() => { cleanup(); localStorage.setItem(STORAGE, "zh"); boot(); });

it("uploads the original file without reading or parsing it in the renderer", async () => {
  const hub = new MockHub();
  const imported = vi.spyOn(hub,"importNativeSession");
  const refresh = vi.fn();
  const archive = {format:"reasonix-native-session",version:2,records:[{role:"system",agentComposition:{digest:"unchanged"}}]};
  const file = new File([JSON.stringify(archive)],"session.json",{type:"application/json"});
  const read = vi.fn().mockRejectedValue(new Error("renderer must not read the archive"));
  Object.defineProperty(file,"text",{value:read});
  render(<NativeSessionImport hub={hub} workspace="/workspace" onImported={refresh}/>);
  await userEvent.upload(screen.getByLabelText("选择会话 JSON 文件"),file);
  expect((await screen.findByRole("status")).textContent).toContain("绑定版本保持不变");
  expect(imported).toHaveBeenCalledWith(file,"/workspace");
  expect(read).not.toHaveBeenCalled();
  expect(refresh).toHaveBeenCalledOnce();
});

it.each(["zh", "en"])("localizes a malformed upload by host code in %s", async (language) => {
  localStorage.setItem(STORAGE, language); boot();
  const hub = new MockHub();
  const refresh = vi.fn();
  vi.spyOn(hub, "importNativeSession").mockRejectedValue(new HttpError(400,
    "SyntaxError: unexpected byte", { code: "session.archive_invalid", error: "raw parser detail" }));
  const file = new File(["{broken"], "broken.json", { type: "application/json" });
  render(<NativeSessionImport hub={hub} workspace="/workspace" onImported={refresh}/>);
  const label = language === "zh" ? "选择会话 JSON 文件" : "Choose a session JSON file";
  await userEvent.upload(screen.getByLabelText(label), file);
  expect((await screen.findByRole("alert")).textContent).toBe(language === "zh"
    ? "会话文件无效，请选择完整的原生会话导出文件。"
    : "Invalid session file. Choose a complete native session export.");
  expect(refresh).not.toHaveBeenCalled();
});

it("offers the file through the app's own button, with the native input out of sight", async () => {
  const hub = new MockHub();
  render(<NativeSessionImport hub={hub} workspace="/workspace" onImported={vi.fn()}/>);
  const input = screen.getByLabelText("选择会话 JSON 文件") as HTMLInputElement;
  expect(input.hidden).toBe(true);
  const open = vi.spyOn(input, "click");
  await userEvent.click(screen.getByRole("button", { name: "选择会话文件…" }));
  expect(open).not.toHaveBeenCalled();
  const dialog = await screen.findByRole("dialog");
  expect(dialog.textContent).toContain("把会话 JSON 文件拖到这里");
  await userEvent.click(screen.getByRole("button", { name: "选择会话 JSON 文件" }));
  expect(open).toHaveBeenCalledOnce();
});

it("takes a file dropped on the dashed area and closes the dialog", async () => {
  const hub = new MockHub();
  const imported = vi.spyOn(hub, "importNativeSession");
  render(<NativeSessionImport hub={hub} workspace="/workspace" onImported={vi.fn()}/>);
  await userEvent.click(screen.getByRole("button", { name: "选择会话文件…" }));
  const drop = document.querySelector(".ns-drop")!;
  const file = new File(["{}"], "s.json", { type: "application/json" });
  fireEvent.drop(drop, { dataTransfer: { files: [file] } });
  await screen.findByRole("status");
  expect(imported).toHaveBeenCalledWith(file, "/workspace");
  expect(screen.queryByRole("dialog")).toBeNull();
});

it("closes the dialog with Cancel and with Escape without importing anything", async () => {
  const hub = new MockHub();
  const imported = vi.spyOn(hub, "importNativeSession");
  render(<NativeSessionImport hub={hub} workspace="/workspace" onImported={vi.fn()}/>);
  await userEvent.click(screen.getByRole("button", { name: "选择会话文件…" }));
  await userEvent.click(screen.getByRole("button", { name: "取消" }));
  expect(screen.queryByRole("dialog")).toBeNull();
  await userEvent.click(screen.getByRole("button", { name: "选择会话文件…" }));
  await userEvent.keyboard("{Escape}");
  expect(screen.queryByRole("dialog")).toBeNull();
  expect(imported).not.toHaveBeenCalled();
});

it("holds the button while an import is running", async () => {
  const hub = new MockHub();
  let finish: () => void = () => {};
  vi.spyOn(hub, "importNativeSession").mockImplementation(() => new Promise<never>((_, reject) => { finish = () => reject(new Error("stop")); }));
  render(<NativeSessionImport hub={hub} workspace="/workspace" onImported={vi.fn()}/>);
  await userEvent.upload(screen.getByLabelText("选择会话 JSON 文件"), new File(["{}"], "s.json", { type: "application/json" }));
  const busy = await screen.findByRole("button", { name: "正在导入…" });
  expect((busy as HTMLButtonElement).disabled).toBe(true);
  finish();
});
