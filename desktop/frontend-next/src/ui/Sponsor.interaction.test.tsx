// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Sponsor } from "./Sponsor";
import type { AgentPort } from "../port/port";

afterEach(cleanup);

const portWith = (openExternal = vi.fn(async () => {})) => ({ openExternal }) as unknown as Pick<AgentPort, "openExternal">;

describe("sponsor", () => {
  it("states the non-binding terms and keeps the QR codes hidden by default", () => {
    render(<Sponsor port={portWith()} />);
    expect(screen.getByText(/不是合同/)).toBeTruthy();
    expect(screen.getByRole("button", { name: "显示收款码" }).getAttribute("aria-expanded")).toBe("false");
    expect(screen.queryByRole("img")).toBeNull();
  });

  it("shows the WeChat Pay and Alipay codes on request and hides them again", async () => {
    render(<Sponsor port={portWith()} />);
    const toggle = screen.getByRole("button", { name: "显示收款码" });
    await userEvent.click(toggle);
    expect(toggle.getAttribute("aria-expanded")).toBe("true");
    const imgs = screen.getAllByRole("img") as HTMLImageElement[];
    expect(imgs.map((i) => i.alt)).toEqual(["微信支付收款码", "支付宝收款码"]);
    for (const img of imgs) expect(img.getAttribute("src")).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "收起收款码" }));
    expect(screen.queryByRole("img")).toBeNull();
  });

  it("opens PayPal through the host", async () => {
    const openExternal = vi.fn(async () => {});
    render(<Sponsor port={portWith(openExternal)} />);
    const link = screen.getByRole("link", { name: "PayPal" });
    expect(link.getAttribute("href")).toBe("https://paypal.me/yuhuahui");
    await userEvent.click(link);
    expect(openExternal).toHaveBeenCalledWith("https://paypal.me/yuhuahui");
  });

  it("says so, with the address, when the host cannot open it", async () => {
    render(<Sponsor port={portWith(vi.fn(async () => { throw new Error("no browser"); }))} />);
    await userEvent.click(screen.getByRole("link", { name: "PayPal" }));
    expect((await screen.findByRole("alert")).textContent).toContain("https://paypal.me/yuhuahui");
  });
});
