// @vitest-environment jsdom
import "./testkit";
import { act, cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { Settings } from "./Settings";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";

afterEach(cleanup);

function draw(port = new MockPort()) {
  const onClose = vi.fn();
  render(<Settings hub={new MockHub()} port={port} status={null} theme="light"
    onTheme={() => {}} contrast="" onContrast={() => {}} weight="" onWeight={() => {}}
    look={{}} onLook={() => {}} reloadThemes={() => {}} onClose={onClose}
    onChanged={() => {}} onError={() => {}} at="persona:agent-presets" account={null}
    accountUnread="" reloadAccount={() => {}} />);
  return { onClose, port };
}

it.each(["close", "navigation", "search"])("protects a draft on settings %s", async (destination) => {
  const user = userEvent.setup();
  const { onClose } = draw();
  expect(screen.getByRole("tab", { name: /^Agent 行为/ })).toBeTruthy();
  await user.click(await screen.findByRole("button", { name: "新建预设" }));
  await user.type(screen.getByLabelText("预设名称"), "保留草稿");
  const leave = async () => {
    if (destination === "close") await user.click(screen.getByRole("button", { name: /关闭.*Esc/ }));
    else if (destination === "navigation") await user.click(screen.getAllByRole<HTMLButtonElement>("tab").find(el => el.id === "prefs-model")!);
    else {
      const search = screen.getByRole("textbox", { name: "搜索设置" });
      await user.clear(search);
      await user.type(search, "模型");
      await user.click(screen.getAllByRole<HTMLButtonElement>("option").find(el => el.dataset.target === "model")!);
    }
  };
  await leave();
  await screen.findByRole("alertdialog");
  await user.keyboard("{Escape}");
  expect(screen.queryByRole("alertdialog")).toBeNull();
  expect((screen.getByLabelText("预设名称") as HTMLInputElement).value).toBe("保留草稿");
  expect(onClose).not.toHaveBeenCalled();
  await leave();
  await user.click(screen.getByRole("button", { name: "放弃更改" }));
  if (destination === "close") expect(onClose).toHaveBeenCalledOnce();
  else expect(screen.queryByRole("form", { name: "新建预设" })).toBeNull();
});

it("uses Escape to return to the catalog without closing Settings", async () => {
  const user = userEvent.setup();
  const { onClose } = draw();
  await user.click(await screen.findByRole("button", { name: "新建预设" }));
  await user.keyboard("{Escape}");
  expect(screen.getByRole("button", { name: "新建预设" })).toBeTruthy();
  expect(onClose).not.toHaveBeenCalled();
});

it("keeps a pending save mounted when Settings is closed", async () => {
  const user = userEvent.setup();
  const port = new MockPort();
  const save = port.saveAgentPreset.bind(port);
  let complete!: () => void;
  vi.spyOn(port, "saveAgentPreset").mockImplementation((draft, revision) =>
    new Promise(resolve => { complete = () => { void save(draft, revision).then(resolve); }; }));
  const { onClose } = draw(port);
  await user.click(await screen.findByRole("button", { name: "新建预设" }));
  await user.type(screen.getByLabelText("预设名称"), "保存中");
  await user.click(screen.getByRole("button", { name: "保存预设" }));
  await user.click(screen.getByRole("button", { name: /关闭.*Esc/ }));
  expect(onClose).not.toHaveBeenCalled();
  expect(screen.getByRole<HTMLButtonElement>("button", { name: "取消" }).disabled).toBe(true);
  await act(async () => complete());
  expect(await screen.findByRole("button", { name: "编辑 保存中" })).toBeTruthy();
});
