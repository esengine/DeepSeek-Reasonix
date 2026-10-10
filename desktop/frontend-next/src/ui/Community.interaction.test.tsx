// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Community } from "./Community";
import { AuthorLine } from "./AuthorLine";
import { profileUrl } from "./communityLinks";
import contributors from "../data/contributors.json";
import type { AgentPort } from "../port/port";

const QR = import.meta.glob("../assets/qq-group-qr.svg", { query: "?raw", import: "default", eager: true }) as Record<string, string>;

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

const portWith = (openExternal = vi.fn(async () => {})) => ({ openExternal }) as unknown as Pick<AgentPort, "openExternal">;

describe("community and contributors", () => {
  it("names the group and shows its number", () => {
    render(<Community port={portWith()} logins={["a"]} />);
    expect(screen.getByText("DeepSeek-Reasonix官方群")).toBeTruthy();
    expect(screen.getByText("1093562660")).toBeTruthy();
  });

  it("opens the join link, Discord and issues through the host", async () => {
    const openExternal = vi.fn(async () => {});
    render(<Community port={portWith(openExternal)} logins={["a"]} />);
    await userEvent.click(screen.getByRole("button", { name: "加入 QQ 群" }));
    expect(openExternal).toHaveBeenLastCalledWith("https://qm.qq.com/q/i59b0z2R8s");
    await userEvent.click(screen.getByRole("button", { name: "Discord" }));
    expect(openExternal).toHaveBeenLastCalledWith("https://discord.gg/XF78rEME2D");
    await userEvent.click(screen.getByRole("button", { name: "GitHub issues" }));
    expect(openExternal).toHaveBeenLastCalledWith("https://github.com/esengine/DeepSeek-Reasonix/issues");
  });

  it("says so, with the address, when the host cannot open it", async () => {
    const openExternal = vi.fn(async () => { throw new Error("no browser"); });
    render(<Community port={portWith(openExternal)} logins={[]} />);
    await userEvent.click(screen.getByRole("button", { name: "Discord" }));
    expect((await screen.findByRole("alert")).textContent).toContain("https://discord.gg/XF78rEME2D");
  });

  it("copies the group number", async () => {
    const writeText = vi.fn(async () => {});
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
    render(<Community port={portWith()} logins={[]} />);
    await userEvent.click(screen.getByRole("button", { name: "复制群号" }));
    await waitFor(() => expect(writeText).toHaveBeenCalledWith("1093562660"));
  });

  it("references a QR image that exists", () => {
    render(<Community port={portWith()} logins={[]} />);
    const img = screen.getByRole("img", { name: "QQ 群二维码" }) as HTMLImageElement;
    expect(img.getAttribute("src")).toBeTruthy();
    expect(Object.values(QR)[0]).toMatch(/^<svg /);
  });

  it("shows the Douyin ID, copies it, and keeps the QR hidden until asked", async () => {
    const writeText = vi.fn(async () => {});
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
    render(<Community port={portWith()} logins={[]} />);
    expect(screen.getByText("做游戏的小鱼")).toBeTruthy();
    expect(screen.getByText("22703872788")).toBeTruthy();
    expect(screen.queryByRole("img", { name: "抖音二维码" })).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "复制抖音号" }));
    await waitFor(() => expect(writeText).toHaveBeenCalledWith("22703872788"));
    const toggle = screen.getByRole("button", { name: "显示二维码" });
    await userEvent.click(toggle);
    expect(toggle.getAttribute("aria-expanded")).toBe("true");
    expect((screen.getByRole("img", { name: "抖音二维码" }) as HTMLImageElement).getAttribute("src")).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "收起二维码" }));
    expect(screen.queryByRole("img", { name: "抖音二维码" })).toBeNull();
  });

  it("has no Douyin link, only the ID", () => {
    const { container } = render(<Community port={portWith()} logins={[]} />);
    expect(container.innerHTML).not.toMatch(/douyin\.com/);
  });

  it("links the author to their GitHub profile through the host", async () => {
    const openExternal = vi.fn(async () => {});
    render(<AuthorLine port={portWith(openExternal)} />);
    expect(screen.getByText("作者与维护者")).toBeTruthy();
    const link = screen.getByRole("link", { name: "esengine" });
    expect(link.getAttribute("href")).toBe("https://github.com/esengine");
    await userEvent.click(link);
    expect(openExternal).toHaveBeenCalledWith("https://github.com/esengine");
  });

  it("lists contributors as links to their exact profiles", async () => {
    const openExternal = vi.fn(async () => {});
    render(<Community port={portWith(openExternal)} logins={["SivanCola", "o-n-e"]} />);
    const link = screen.getByRole("link", { name: "SivanCola" });
    expect(link.getAttribute("href")).toBe("https://github.com/SivanCola");
    await userEvent.click(link);
    expect(openExternal).toHaveBeenLastCalledWith("https://github.com/SivanCola");
    await userEvent.click(screen.getByRole("button", { name: "查看全部贡献者" }));
    expect(openExternal).toHaveBeenLastCalledWith("https://github.com/esengine/DeepSeek-Reasonix/graphs/contributors");
  });

  it("never turns a hostile login into a URL", () => {
    const hostile = ["a/b", 'x"y', "../../evil", "-lead", "trail-", "a--b", "", "x".repeat(40), "https://evil.test"];
    for (const login of hostile) expect(profileUrl(login)).toBeNull();
    expect(profileUrl("A-b1")).toBe("https://github.com/A-b1");
    render(<Community port={portWith()} logins={[...hostile, "good"]} />);
    const people = screen.getByRole("list", { name: "贡献者" });
    expect(within(people).getAllByRole("link").map((a) => a.getAttribute("href"))).toEqual(["https://github.com/good"]);
  });

  it("collapses a long list and expands it on request", async () => {
    const logins = Array.from({ length: 60 }, (_, i) => `user${i}`);
    render(<Community port={portWith()} logins={logins} />);
    const people = screen.getByRole("list", { name: "贡献者" });
    expect(within(people).getAllByRole("link")).toHaveLength(24);
    await userEvent.click(screen.getByRole("button", { name: "显示全部 60 位" }));
    expect(within(people).getAllByRole("link")).toHaveLength(60);
    await userEvent.click(screen.getByRole("button", { name: "收起" }));
    expect(within(people).getAllByRole("link")).toHaveLength(24);
  });

  it("offers no toggle for a short list", () => {
    render(<Community port={portWith()} logins={["a", "b"]} />);
    expect(screen.queryByRole("button", { name: /显示全部/ })).toBeNull();
  });

  it("ships a contributors file whose every entry is a valid login", () => {
    expect(contributors.length).toBeGreaterThan(0);
    for (const login of contributors) expect(profileUrl(login)).not.toBeNull();
    expect(contributors.map((l) => l.toLowerCase())).not.toContain("esengine");
  });
});
