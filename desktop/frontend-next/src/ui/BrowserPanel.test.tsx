// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { BrowserPanel } from "./BrowserPanel";

afterEach(cleanup);

describe("BrowserPanel", () => {
  it("opens a typed address when Enter is pressed", async () => {
    const user = userEvent.setup();
    const external = vi.fn();
    render(<BrowserPanel id="b0" scheme="light" onAddress={vi.fn()} onExternal={external} />);
    expect(screen.getByTitle("Reasonix 内置 Browser").querySelector(".lucide-globe")).not.toBeNull();

    const address = screen.getByRole("textbox", { name: "网页地址" });
    await user.clear(address);
    await user.type(address, "example.com/docs{Enter}");

    expect((address as HTMLInputElement).value).toBe("https://example.com/docs");
    expect(screen.getByTitle("Reasonix 内置 Browser").getAttribute("src")).toBe("https://example.com/docs");
    const toolbar = document.querySelector(".studio-browser-toolbar")!;
    const outside = toolbar.querySelector<HTMLButtonElement>('[data-action="browser.external"]')!;
    expect(outside.querySelector(".lucide-external-link")).not.toBeNull();
    await user.click(outside);
    expect(external).toHaveBeenCalledWith("https://example.com/docs");
    const frame = screen.getByTitle("Reasonix 内置 Browser");
    await user.click(toolbar.querySelector('[data-action="browser.reload"]')!);
    expect(screen.getByTitle("Reasonix 内置 Browser")).not.toBe(frame);
    await user.click(toolbar.querySelector('[data-action="browser.back"]')!);
    expect((address as HTMLInputElement).value).toBe("reasonix://start");
    expect(outside.disabled).toBe(true);
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

  it.each(["D:/DevCode/aglo.html", "D:\\DevCode\\aglo.html", "\\\\nas\\share\\x.html", "/Users/me/index.html"])(
    "refuses the path %s instead of reading its drive letter as a host",
    async (path) => {
      const user = userEvent.setup();
      render(<BrowserPanel id="b0" scheme="light" onAddress={vi.fn()} onExternal={vi.fn()} />);
      const address = screen.getByRole("textbox", { name: "网页地址" });
      await user.clear(address);
      await user.type(address, `${path.replace(/[{[]/g, "$&$&")}{Enter}`);
      expect(screen.getByRole("alert").textContent).toContain("http 或 https");
      expect(screen.getByTitle("Reasonix 内置 Browser").hasAttribute("src")).toBe(false);
    },
  );

  it("still opens a host and port, and a bare host, as web addresses", async () => {
    const user = userEvent.setup();
    render(<BrowserPanel id="b0" scheme="light" onAddress={vi.fn()} onExternal={vi.fn()} />);
    const address = screen.getByRole("textbox", { name: "网页地址" });
    await user.clear(address);
    await user.type(address, "localhost:3000/x{Enter}");
    expect(screen.getByTitle("Reasonix 内置 Browser").getAttribute("src")).toBe("http://localhost:3000/x");
  });
});
