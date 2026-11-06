// @vitest-environment jsdom
import "./testkit";
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { SettingsUnavailable } from "./SettingsUnavailable";

afterEach(cleanup);

it("says what failed, why, and what it spares, with reload as the main action", () => {
  render(<SettingsUnavailable onClose={() => {}} />);
  const dialog = screen.getByRole("alertdialog");
  expect(dialog.querySelector("h1")?.textContent).toBe("设置没能打开");
  expect(dialog.textContent).toContain("这一页是单独取的，这次没取到");
  expect(dialog.textContent).toContain("当前会话与正在跑的回合都不受影响");
  const actions = [...dialog.querySelectorAll(".gone-acts button")];
  expect(actions.map((b) => b.textContent)).toEqual(["关闭", "重新载入"]);
  expect(actions[1].hasAttribute("data-primary")).toBe(true);
});

it("closes through the ghost action", async () => {
  const onClose = vi.fn();
  render(<SettingsUnavailable onClose={onClose} />);
  await userEvent.click(screen.getByText("关闭"));
  expect(onClose).toHaveBeenCalledOnce();
});
