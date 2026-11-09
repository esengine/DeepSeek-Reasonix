// @vitest-environment jsdom
import "./testkit";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { Settings } from "./Settings";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";

afterEach(cleanup);

function draw(at: string) {
  render(<Settings hub={new MockHub()} port={new MockPort()} status={null} at={at}
    theme="light" onTheme={() => {}} contrast="" onContrast={() => {}} weight="" onWeight={() => {}}
    look={{}} onLook={() => {}} reloadThemes={() => {}} onClose={vi.fn()} onChanged={() => {}} onError={() => {}}
    account={null} accountUnread="" reloadAccount={() => {}} />);
}

it("shows appearance controls together and keeps them mounted when a pill locates a region", async () => {
  draw("appearance");
  const font = await screen.findByRole("combobox", { name: "界面" });
  expect(document.querySelector('#set-wallpaper input[type="file"]')).toBeTruthy();
  expect(document.querySelector('#set-size input[type="range"]')).toBeTruthy();
  await userEvent.click(document.querySelector('[data-action="settings.page-index"][data-target="size"]')!);
  expect(screen.getByRole("combobox", { name: "界面" })).toBe(font);
  expect(document.querySelector(".deep-page")).toBeNull();
});

it("shows every notification control without a disclosure", async () => {
  draw("common");
  expect(await screen.findByRole("switch", { name: "播放提示音" })).toBeTruthy();
  expect(screen.getByRole("switch", { name: "磁盘空间不足时" })).toBeTruthy();
  expect(document.querySelector('[data-action="notify.more"]')).toBeNull();
});

it("keeps an unsaved preset mounted when a pill scrolls within the same category", async () => {
  draw("session");
  await userEvent.click(await screen.findByRole("button", { name: "新建预设" }));
  const name = screen.getByRole("textbox", { name: "预设名称" });
  await userEvent.type(name, "保留编辑内容");
  await userEvent.click(document.querySelector('[data-action="settings.page-index"][data-target="ask-timeout"]')!);
  await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
  expect(screen.getByRole("textbox", { name: "预设名称" })).toBe(name);
  expect((name as HTMLInputElement).value).toBe("保留编辑内容");
});
