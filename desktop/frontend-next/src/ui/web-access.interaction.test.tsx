// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { WebAccess } from "./WebAccess";
import { MockPort } from "../port/mock";
import { HttpError } from "../port/http_error";
import { boot, STORAGE } from "../i18n";
import type { AgentPort, WebAccessSettings } from "../port/port";

beforeEach(() => {
  localStorage.setItem(STORAGE, "zh");
  boot();
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

const settings = (over: Partial<WebAccessSettings> = {}): WebAccessSettings => ({
  search_backends: [], searxng_url: "", search_paid_allowed: [], fetch_backends: [], fetch_paid_allowed: [],
  known_search_backends: ["searxng", "deepseek_api"], known_fetch_backends: ["local", "deepseek_api"], paid_backends: ["deepseek_api"],
  deepseek_api_ready: true, path: "/home/me/.reasonix/config.toml", ...over,
});

function portWith(initial: WebAccessSettings) {
  const port = new MockPort();
  let current = initial;
  vi.spyOn(port, "webAccessSettings").mockImplementation(async () => current);
  const save = vi.spyOn(port, "saveWebAccessSettings").mockImplementation(async (patch) => {
    current = { ...current, ...patch };
    return current;
  });
  return { port: port as unknown as AgentPort, save };
}

const search = () => screen.getByRole("group", { name: "网页搜索" });
const fetchGroup = () => screen.getByRole("group", { name: "网页抓取" });

describe("web search and fetch settings", () => {
  it("says what an empty list means instead of leaving it blank", async () => {
    const { port } = portWith(settings());
    render(<WebAccess port={port} />);
    await screen.findByRole("group", { name: "网页搜索" });
    expect(within(search()).getByText(/沿用模型自带的搜索/)).toBeTruthy();
    expect(within(fetchGroup()).getByText(/只用本机抓取/)).toBeTruthy();
  });

  it("turns a backend on by adding it to the end of the ordered list", async () => {
    const { port, save } = portWith(settings({ search_backends: ["searxng"], searxng_url: "http://127.0.0.1:8080" }));
    render(<WebAccess port={port} />);
    await userEvent.click(await within(await screen.findByRole("group", { name: "网页搜索" })).findByRole("switch", { name: "启用 DeepSeek 官方搜索" }));
    expect(save).toHaveBeenCalledExactlyOnceWith({ search_backends: ["searxng", "deepseek_api"] });
  });

  it("turns a backend off by removing it, and an empty list is saved as empty", async () => {
    const { port, save } = portWith(settings({ search_backends: ["searxng"], searxng_url: "http://x" }));
    render(<WebAccess port={port} />);
    await userEvent.click(await within(await screen.findByRole("group", { name: "网页搜索" })).findByRole("switch", { name: "启用 SearXNG" }));
    expect(save).toHaveBeenCalledExactlyOnceWith({ search_backends: [] });
  });

  it("moves an enabled backend up and down, since the order is the order of trying", async () => {
    const { port, save } = portWith(settings({ search_backends: ["searxng", "deepseek_api"], searxng_url: "http://x" }));
    render(<WebAccess port={port} />);
    const group = await screen.findByRole("group", { name: "网页搜索" });
    expect((within(group).getByRole("button", { name: "上移 SearXNG" }) as HTMLButtonElement).disabled).toBe(true);
    await userEvent.click(within(group).getByRole("button", { name: "上移 DeepSeek 官方搜索" }));
    expect(save).toHaveBeenCalledExactlyOnceWith({ search_backends: ["deepseek_api", "searxng"] });
  });

  it("saves the SearXNG address on its own when the field is left", async () => {
    const { port, save } = portWith(settings({ search_backends: ["searxng"] }));
    render(<WebAccess port={port} />);
    const field = await screen.findByRole("textbox", { name: "SearXNG 地址" });
    await userEvent.type(field, "http://127.0.0.1:8080");
    await userEvent.tab();
    expect(save).toHaveBeenCalledExactlyOnceWith({ searxng_url: "http://127.0.0.1:8080" });
  });

  it("shows a refused address beside the field, worded from the kernel's reply", async () => {
    const { port } = portWith(settings({ search_backends: ["searxng"] }));
    vi.spyOn(port, "saveWebAccessSettings").mockRejectedValue(new HttpError(400, "bad", { code: "web_access.bad_searxng_url", error: "that is not an http address" }));
    render(<WebAccess port={port} />);
    await userEvent.type(await screen.findByRole("textbox", { name: "SearXNG 地址" }), "nope");
    await userEvent.tab();
    expect((await screen.findByRole("alert")).textContent).toContain("SearXNG 地址必须是带主机名的");
  });
});

describe("paid backends and the consent to be billed", () => {
  it("never ticks the consent for the person, and names the cost", async () => {
    const { port } = portWith(settings({ search_backends: ["deepseek_api"] }));
    render(<WebAccess port={port} />);
    const consent = await screen.findByRole("switch", { name: "同意网页搜索按次计费" });
    expect(consent.getAttribute("aria-checked")).toBe("false");
    expect(screen.getAllByText(/计费/).length).toBeGreaterThan(0);
  });

  it("writes the consent only from its own switch, and takes it back the same way", async () => {
    const { port, save } = portWith(settings({ search_backends: ["deepseek_api"] }));
    render(<WebAccess port={port} />);
    await userEvent.click(await screen.findByRole("switch", { name: "同意网页搜索按次计费" }));
    expect(save).toHaveBeenLastCalledWith({ search_paid_allowed: ["deepseek_api"] });
    await waitFor(() => expect(screen.getByRole("switch", { name: "同意网页搜索按次计费" }).getAttribute("aria-checked")).toBe("true"));
    await userEvent.click(screen.getByRole("switch", { name: "同意网页搜索按次计费" }));
    expect(save).toHaveBeenLastCalledWith({ search_paid_allowed: [] });
  });

  it("keeps search consent and fetch consent apart", async () => {
    const { port, save } = portWith(settings({ fetch_backends: ["local", "deepseek_api"] }));
    render(<WebAccess port={port} />);
    await userEvent.click(await screen.findByRole("switch", { name: "同意网页抓取按次计费" }));
    expect(save).toHaveBeenCalledExactlyOnceWith({ fetch_paid_allowed: ["deepseek_api"] });
  });

  it("holds the consent inert, and says why, without a DeepSeek provider that has a key", async () => {
    const { port, save } = portWith(settings({ search_backends: ["deepseek_api"], deepseek_api_ready: false }));
    render(<WebAccess port={port} />);
    const consent = (await screen.findByRole("switch", { name: "同意网页搜索按次计费" })) as HTMLButtonElement;
    expect(consent.disabled).toBe(true);
    expect(screen.getAllByText(/需要一个带密钥的 DeepSeek 提供方/).length).toBeGreaterThan(0);
    await userEvent.click(consent);
    expect(save).not.toHaveBeenCalled();
  });

  it("still lets a consent already given be withdrawn when the key is gone", async () => {
    const { port, save } = portWith(settings({ search_backends: ["deepseek_api"], search_paid_allowed: ["deepseek_api"], deepseek_api_ready: false }));
    render(<WebAccess port={port} />);
    const consent = (await screen.findByRole("switch", { name: "同意网页搜索按次计费" })) as HTMLButtonElement;
    expect(consent.disabled).toBe(false);
    await userEvent.click(consent);
    expect(save).toHaveBeenCalledExactlyOnceWith({ search_paid_allowed: [] });
  });
});

describe("a window that was not granted editing", () => {
  const refused = () => new HttpError(403, "x", { code: "web_access.editing_disabled", error: "editing is not enabled" });

  it("shows the settings read-only after the first refusal, with the kernel's words", async () => {
    const { port } = portWith(settings({ search_backends: ["deepseek_api"] }));
    vi.spyOn(port, "saveWebAccessSettings").mockRejectedValue(refused());
    render(<WebAccess port={port} />);
    await userEvent.click(await screen.findByRole("switch", { name: "同意网页搜索按次计费" }));
    expect((await screen.findByRole("alert")).textContent).toContain("只有这台电脑上的窗口能改");
    const group = screen.getByRole("group", { name: "网页搜索" });
    for (const control of within(group).getAllByRole("switch")) expect((control as HTMLButtonElement).disabled).toBe(true);
    for (const button of within(group).getAllByRole("button")) expect((button as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByText(/这些设置现在是只读的/)).toBeTruthy();
  });

  it("holds the address field and the fetch side read-only too, and does not try again", async () => {
    const { port } = portWith(settings({ search_backends: ["searxng"], searxng_url: "http://x", fetch_backends: ["local"] }));
    const save = vi.spyOn(port, "saveWebAccessSettings").mockRejectedValue(refused());
    render(<WebAccess port={port} />);
    await userEvent.click(await screen.findByRole("switch", { name: "启用 SearXNG" }));
    await screen.findByRole("alert");
    expect((screen.getByRole("textbox", { name: "SearXNG 地址" }) as HTMLInputElement).disabled).toBe(true);
    for (const control of within(screen.getByRole("group", { name: "网页抓取" })).getAllByRole("switch")) expect((control as HTMLButtonElement).disabled).toBe(true);
    await userEvent.click(screen.getByRole("switch", { name: "启用 SearXNG" }));
    expect(save).toHaveBeenCalledTimes(1);
  });

  it("any other refusal leaves the controls live", async () => {
    const { port } = portWith(settings({ search_backends: ["searxng"], searxng_url: "http://x" }));
    vi.spyOn(port, "saveWebAccessSettings").mockRejectedValue(new HttpError(500, "x", { code: "web_access.save_failed", error: "disk full" }));
    render(<WebAccess port={port} />);
    await userEvent.click(await screen.findByRole("switch", { name: "启用 SearXNG" }));
    await screen.findByRole("alert");
    expect((screen.getByRole("switch", { name: "启用 SearXNG" }) as HTMLButtonElement).disabled).toBe(false);
    expect(screen.queryByText(/这些设置现在是只读的/)).toBeNull();
  });
});

describe("when the settings apply", () => {
  it("says a saved change reaches panes opened afterwards, and names the file", async () => {
    const { port } = portWith(settings());
    render(<WebAccess port={port} />);
    await screen.findByRole("group", { name: "网页搜索" });
    expect(screen.getByText(/之后新开的窗格/)).toBeTruthy();
    expect(screen.getByText("/home/me/.reasonix/config.toml")).toBeTruthy();
  });

  it("tests the address in the field and names why it fails", async () => {
    const { port, save } = portWith(settings({ search_backends: ["searxng"], searxng_url: "http://127.0.0.1:8080" }));
    const probe = vi.spyOn(port, "probeSearxng").mockResolvedValue({ ok: false, code: "search.searxng_json_disabled", status: 403 });
    render(<WebAccess port={port} />);
    const field = await screen.findByRole("textbox", { name: "SearXNG 地址" });
    await userEvent.clear(field);
    await userEvent.type(field, "http://127.0.0.1:9999");
    await userEvent.click(screen.getByRole("button", { name: "测试连接" }));
    expect(probe).toHaveBeenCalledExactlyOnceWith("http://127.0.0.1:9999");
    expect((await screen.findByRole("status")).textContent).toMatch(/search\.formats/);
    for (const [patch] of save.mock.calls) expect(Object.keys(patch)).toEqual(["searxng_url"]);
  });

  it("says plainly when the instance answers", async () => {
    const { port } = portWith(settings({ search_backends: ["searxng"], searxng_url: "http://127.0.0.1:8080" }));
    vi.spyOn(port, "probeSearxng").mockResolvedValue({ ok: true });
    render(<WebAccess port={port} />);
    await userEvent.click(await screen.findByRole("button", { name: "测试连接" }));
    expect((await screen.findByRole("status")).textContent).toMatch(/连通/);
  });

  it("cannot test an empty address, and a refused test locks the section as read-only", async () => {
    const { port } = portWith(settings({ search_backends: ["searxng"] }));
    vi.spyOn(port, "probeSearxng").mockRejectedValue(new HttpError(403, "x", { code: "web_access.editing_disabled", error: "editing is not enabled" }));
    render(<WebAccess port={port} />);
    const button = await screen.findByRole("button", { name: "测试连接" });
    expect((button as HTMLButtonElement).disabled).toBe(true);
    await userEvent.type(screen.getByRole("textbox", { name: "SearXNG 地址" }), "http://x");
    await userEvent.click(button);
    expect(await screen.findByText(/只读/)).toBeTruthy();
  });
});
