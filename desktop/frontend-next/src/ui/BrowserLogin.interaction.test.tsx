// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import "./testkit";
import type { BrowserLogin as Ask } from "../port/host";

const shell = vi.hoisted(() => ({ listener: null as null | ((a: Ask) => void), answer: vi.fn() }));
vi.mock("../port/host", () => ({
  host: () => ({
    onBrowserLogin: (fn: (a: Ask) => void) => { shell.listener = fn; return () => {}; },
    answerBrowserLogin: shell.answer,
  }),
}));
import { BrowserLogin } from "./BrowserLogin";

afterEach(() => { cleanup(); shell.answer.mockReset(); });

function ask(over: Partial<Ask> = {}) {
  render(<BrowserLogin />);
  act(() => shell.listener!({ id: "a1", host: "intranet.corp", realm: "Staff only", proxy: false, ...over } as Ask));
}

it("names the site, repeats its prompt, and says where the password goes", () => {
  ask();
  const dialog = screen.getByRole("dialog", { name: "intranet.corp 需要登录" });
  expect(dialog.textContent).toContain("站点提示：Staff only。");
  expect(dialog.textContent).toContain("用户名和密码只交给这个网页");
  expect(screen.getByLabelText("用户名")).toBeTruthy();
  expect(screen.getByLabelText("密码")).toBeTruthy();
});

it("titles a proxy as a proxy, with its port", () => {
  ask({ proxy: true, host: "proxy.corp", port: 8080, realm: "" });
  expect(screen.getByRole("dialog", { name: "代理 proxy.corp:8080 需要登录" })).toBeTruthy();
  expect(screen.queryByText(/站点提示/)).toBeNull();
});

it("signs in with what was typed, and the close button cancels like Cancel does", () => {
  ask();
  fireEvent.change(screen.getByLabelText("用户名"), { target: { value: "alice" } });
  fireEvent.change(screen.getByLabelText("密码"), { target: { value: "pw" } });
  fireEvent.click(screen.getByRole("button", { name: "登录" }));
  expect(shell.answer).toHaveBeenCalledWith("a1", "alice", "pw");
  ask({ id: "a2" });
  fireEvent.click(screen.getByRole("button", { name: "关闭" }));
  expect(shell.answer).toHaveBeenLastCalledWith("a2", "", "");
});
