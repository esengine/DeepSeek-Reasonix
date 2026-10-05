// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { BrowserPanel } from "./BrowserPanel";

afterEach(cleanup);

describe("BrowserPanel", () => {
  it("opens a typed address when Enter is pressed", async () => {
    const user = userEvent.setup();
    render(<BrowserPanel id="b0" scheme="light" onAddress={vi.fn()} onExternal={vi.fn()} />);

    const address = screen.getByRole("textbox", { name: "网页地址" });
    await user.clear(address);
    await user.type(address, "example.com/docs{Enter}");

    expect((address as HTMLInputElement).value).toBe("https://example.com/docs");
    expect(screen.getByTitle("Reasonix 内置 Browser").getAttribute("src")).toBe("https://example.com/docs");
  });

  it("keeps invalid addresses in the field and explains the error", async () => {
    const user = userEvent.setup();
    render(<BrowserPanel id="b0" scheme="dark" onAddress={vi.fn()} onExternal={vi.fn()} />);

    const address = screen.getByRole("textbox", { name: "网页地址" });
    await user.clear(address);
    await user.type(address, "file:///private{Enter}");

    expect(screen.getByRole("alert").textContent).toContain("请输入有效的 http 或 https 地址");
    expect(screen.getByTitle("Reasonix 内置 Browser").hasAttribute("src")).toBe(false);
  });
});
