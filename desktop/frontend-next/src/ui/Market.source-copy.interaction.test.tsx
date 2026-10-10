// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { boot, current, STORAGE } from "../i18n";
import { MockPort } from "../port/mock";
import type { AgentPort, MarketDetail, MarketKind, MarketPackage } from "../port/port";
import { Market } from "./Market";

const clipboard = Object.getOwnPropertyDescriptor(navigator, "clipboard");
const lang = current();
const storedLang = localStorage.getItem(STORAGE);

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  if (clipboard) Object.defineProperty(navigator, "clipboard", clipboard);
  else Reflect.deleteProperty(navigator, "clipboard");
  localStorage.setItem(STORAGE, lang);
  boot();
  if (storedLang === null) localStorage.removeItem(STORAGE);
  else localStorage.setItem(STORAGE, storedLang);
});

const row = (slug: string, kind: MarketKind = "skill"): MarketPackage => ({
  kind, handle: slug.split("/")[0], name: slug.split("/")[1], slug, summary: "A reusable capability", description: "", homepage: "",
  repoUrl: "https://github.com/team/monorepo", tags: [], latestVersion: "9.0.0", installCount: 3, starCount: 0,
  upCount: 0, downCount: 0, approvalRate: null, score: 0, verified: false, status: "active", updatedAt: "", pinned: true,
});

const detail = (source = "https://example.com/reviewed/SKILL.md", kind: MarketKind = "skill", slug = "team/kit"): MarketDetail => ({
  package: row(slug, kind), pinned: true,
  approved: { version: "1.2.3", source, contentHash: "reviewed-hash", riskLevel: "low", createdAt: "" },
});

const portWith = (...details: MarketDetail[]) => {
  const port = new MockPort() as unknown as AgentPort;
  port.marketList = vi.fn(async () => ({ packages: details.map((d) => d.package), limit: 24, offset: 0 }));
  port.marketDetail = vi.fn(async (slug) => details.find((d) => d.package.slug === slug)!);
  port.marketMyVote = vi.fn(async () => ({ signedIn: false, value: 0 as const }));
  vi.spyOn(port, "planMarket");
  vi.spyOn(port, "installMarket");
  return port;
};

const clipboardWith = (writeText = vi.fn(async (_text: string) => {})) => {
  Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
  return writeText;
};

const open = async (name = "kit") => {
  await userEvent.click(await screen.findByRole("button", { name: new RegExp(name) }));
  return screen.findByRole<HTMLButtonElement>("button", { name: "复制审核来源" });
};

describe("market approved-source copy", () => {
  it.each([
    ["skill", "https://example.com/reviewed/SKILL.md?revision=1.2.3"],
    ["plugin", `https://github.com/team/monorepo/tree/${"a".repeat(40)}/plugins/kit`],
    ["theme", `https://github.com/team/monorepo/tree/${"b".repeat(40)}/themes/kit`],
    ["mcp", "npm:@team/mcp-kit@1.2.3"],
  ] as const)("copies the producer's approved %s source, keeping repository and latest metadata separate", async (kind, source) => {
    const write = clipboardWith();
    const d = detail(source, kind);
    const port = portWith(d);
    const installed = vi.fn();
    render(<Market port={port} onInstalled={installed} />);
    const copy = await open();
    const facts = copy.closest("dl")!;
    expect(within(facts).getByText(source)).toBeTruthy();
    expect(within(facts).getByText(d.package.repoUrl)).toBeTruthy();
    expect(within(facts).getByText("1.2.3")).toBeTruthy();
    expect(within(facts).queryByText("9.0.0")).toBeNull();
    expect(copy.title).toBe("复制审核来源");
    await userEvent.click(copy);
    await waitFor(() => expect(copy.textContent).toBe("已复制"));
    expect(write).toHaveBeenCalledExactlyOnceWith(source);
    expect(copy.getAttribute("aria-label")).toBe("复制审核来源");
    expect(copy.querySelector('[aria-live="polite"]')?.textContent).toBe("已复制");
    expect(port.planMarket).not.toHaveBeenCalled();
    expect(port.installMarket).not.toHaveBeenCalled();
    expect(installed).not.toHaveBeenCalled();
  });

  it("keeps keyboard focus and allows retry after clipboard rejection", async () => {
    const source = "https://example.com/reviewed/SKILL.md";
    const write = clipboardWith(vi.fn().mockRejectedValueOnce(new Error("denied")).mockResolvedValueOnce(undefined));
    const port = portWith(detail(source));
    render(<Market port={port} onInstalled={() => {}} />);
    const copy = await open();
    screen.getByRole("region", { name: "team/kit" }).focus();
    await userEvent.tab();
    expect(document.activeElement).toBe(copy);
    await userEvent.keyboard("{Enter}");
    await waitFor(() => expect(copy.textContent).toBe("复制失败，请重试"));
    expect(copy.title).toBe("复制失败，请重试");
    expect(document.activeElement).toBe(copy);
    expect(screen.getByRole("button", { name: "查看将安装的内容" })).toBeTruthy();
    await userEvent.keyboard(" ");
    await waitFor(() => expect(copy.textContent).toBe("已复制"));
    expect(write.mock.calls).toEqual([[source], [source]]);
    expect(document.activeElement).toBe(copy);
    await userEvent.tab();
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "返回列表" }));
    expect(port.planMarket).not.toHaveBeenCalled();
    expect(port.installMarket).not.toHaveBeenCalled();
  });

  it.each(["no approved version", "empty source"])("offers no copy control with %s even when a repository and latest version exist", async (missing) => {
    const d = detail("");
    if (missing === "no approved version") delete d.approved;
    const write = clipboardWith();
    const port = portWith(d);
    render(<Market port={port} onInstalled={() => {}} />);
    await userEvent.click(await screen.findByRole("button", { name: /kit/ }));
    await screen.findByRole("button", { name: "查看将安装的内容" });
    expect(screen.queryByRole("button", { name: "复制审核来源" })).toBeNull();
    expect(screen.getByText("—")).toBeTruthy();
    expect(screen.getByText(d.package.repoUrl)).toBeTruthy();
    expect(write).not.toHaveBeenCalled();
    expect(port.planMarket).not.toHaveBeenCalled();
  });

  it("copies an unpinned source while retaining the publisher trust warning and explicit install step", async () => {
    const d = { ...detail(), pinned: false };
    const write = clipboardWith();
    const port = portWith(d);
    render(<Market port={port} onInstalled={() => {}} />);
    await userEvent.click(await open());
    await screen.findByText("已复制");
    expect(write).toHaveBeenCalledWith(d.approved!.source);
    expect(screen.getByText("内容未经审核固定")).toBeTruthy();
    expect(screen.getByRole("button", { name: "信任并安装" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "查看将安装的内容" })).toBeNull();
    expect(port.planMarket).not.toHaveBeenCalled();
    expect(port.installMarket).not.toHaveBeenCalled();
  });

  it("starts new copy feedback after returning to the list and opening another approved item", async () => {
    const a = detail();
    const b = detail("https://example.com/other/SKILL.md", "skill", "team/other");
    const write = clipboardWith();
    render(<Market port={portWith(a, b)} onInstalled={() => {}} />);
    await userEvent.click(await open());
    await screen.findByText("已复制");
    await userEvent.click(screen.getByRole("button", { name: "返回列表" }));
    const next = await open("other");
    expect(next.textContent).toBe("复制审核来源");
    await userEvent.click(next);
    await screen.findByText("已复制");
    expect(write.mock.calls).toEqual([[a.approved!.source], [b.approved!.source]]);
  });

  it.each(["success", "failure"])("does not transfer a late clipboard %s to a replacement connection with the same approved source", async (outcome) => {
    let finish!: () => void;
    let fail!: (error: Error) => void;
    const write = clipboardWith(vi.fn(() => new Promise<void>((resolve, reject) => { finish = resolve; fail = reject; })));
    const a = portWith(detail());
    const b = portWith(detail());
    const view = render(<Market port={a} onInstalled={() => {}} />);
    await userEvent.click(await open());
    expect(write).toHaveBeenCalledTimes(1);
    view.rerender(<Market port={b} onInstalled={() => {}} />);
    const next = await screen.findByRole("button", { name: "复制审核来源" });
    expect(next.textContent).toBe("复制审核来源");
    await act(async () => {
      if (outcome === "success") finish();
      else fail(new Error("old clipboard denied"));
    });
    expect(next.textContent).toBe("复制审核来源");
    expect(next.getAttribute("data-state")).toBe("idle");
    expect(screen.queryByText("已复制")).toBeNull();
    expect(screen.queryByText("复制失败，请重试")).toBeNull();
    expect(a.planMarket).not.toHaveBeenCalled();
    expect(b.planMarket).not.toHaveBeenCalled();
    expect(a.installMarket).not.toHaveBeenCalled();
    expect(b.installMarket).not.toHaveBeenCalled();
  });

  it("copies exactly through the existing desktop clipboard fallback", async () => {
    Object.defineProperty(navigator, "clipboard", { value: undefined, configurable: true });
    let captured = "";
    const previous = Object.getOwnPropertyDescriptor(document, "execCommand");
    const exec = vi.fn((command: string) => {
      expect(command).toBe("copy");
      captured = document.querySelector("textarea")!.value;
      return true;
    });
    Object.defineProperty(document, "execCommand", { value: exec, configurable: true });
    const d = detail();
    try {
      render(<Market port={portWith(d)} onInstalled={() => {}} />);
      await userEvent.click(await open());
      await screen.findByText("已复制");
      expect(captured).toBe(d.approved!.source);
      expect(exec).toHaveBeenCalledTimes(1);
      expect(document.querySelector("textarea")).toBeNull();
    } finally {
      if (previous) Object.defineProperty(document, "execCommand", previous);
      else Reflect.deleteProperty(document, "execCommand");
    }
  });

  it("names the approved-source action in English and retains translated success feedback", async () => {
    localStorage.setItem(STORAGE, "en");
    boot();
    const write = clipboardWith();
    const d = detail();
    render(<Market port={portWith(d)} onInstalled={() => {}} />);
    await userEvent.click(await screen.findByRole("button", { name: /kit/ }));
    const copy = await screen.findByRole("button", { name: "Copy approved source" });
    expect(copy.title).toBe("Copy approved source");
    await userEvent.click(copy);
    await screen.findByText("Copied");
    expect(write).toHaveBeenCalledExactlyOnceWith(d.approved!.source);
    expect(copy.getAttribute("aria-label")).toBe("Copy approved source");
  });
});
