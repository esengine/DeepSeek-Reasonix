// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Market } from "./Market";
import { MockPort } from "../port/mock";
import { boot, STORAGE, t } from "../i18n";
import type { AgentPort, MarketCache, MarketPackage } from "../port/port";

afterEach(() => { cleanup(); localStorage.setItem(STORAGE, "zh"); boot(); });

const pkg: MarketPackage = {
  slug: "demo/notes-kit", kind: "plugin", handle: "demo", name: "notes-kit", summary: "Fixture",
  description: "", homepage: "", repoUrl: "", tags: [], latestVersion: "1.0.0", installCount: 0,
  starCount: 0, upCount: 0, downCount: 0, approvalRate: null, score: 0, verified: false, status: "active", updatedAt: "",
};
const cache: MarketCache = { cachedAt: "2026-10-05T08:30:00Z", cause: "unreachable" };

function fixture() {
  const port = new MockPort() as unknown as AgentPort;
  port.marketList = vi.fn().mockResolvedValue({ packages: [pkg], limit: 24, offset: 0 });
  port.marketMyVote = vi.fn().mockResolvedValue({ signedIn: false, value: 0 });
  port.marketDetail = vi.fn().mockResolvedValue({ package: pkg, pinned: true });
  return port;
}

it.each(["zh", "en"])("labels a listing served from the last good copy and leaves a live one unlabelled (%s)", async (lang) => {
  localStorage.setItem(STORAGE, lang); boot();
  const port = fixture();
  (port.marketList as ReturnType<typeof vi.fn>).mockResolvedValueOnce({ packages: [pkg], limit: 24, offset: 0, cache });
  render(<Market port={port} onInstalled={() => {}} />);
  expect(await screen.findByText(t("显示的是缓存数据"))).toBeTruthy();
  expect(screen.getByRole("button", { name: /notes-kit/ })).toBeTruthy();
  await userEvent.click(screen.getByRole("button", { name: t("重试") }));
  expect(await screen.findByRole("button", { name: /notes-kit/ })).toBeTruthy();
  expect(screen.queryByText(t("显示的是缓存数据"))).toBeNull();
  expect(port.marketList).toHaveBeenCalledTimes(2);
});

it("says the registry answered unusably when that is the cause", async () => {
  const port = fixture();
  (port.marketList as ReturnType<typeof vi.fn>).mockResolvedValue({ packages: [pkg], limit: 24, offset: 0, cache: { ...cache, cause: "bad_response" } });
  render(<Market port={port} onInstalled={() => {}} />);
  expect(await screen.findByText(/暂时无法正常回应/)).toBeTruthy();
  expect(screen.queryByText(/无法连接社区市场，/)).toBeNull();
});

it("drops the label when a filter change reaches the live registry", async () => {
  const port = fixture();
  (port.marketList as ReturnType<typeof vi.fn>).mockResolvedValueOnce({ packages: [pkg], limit: 24, offset: 0, cache });
  render(<Market port={port} onInstalled={() => {}} />);
  await screen.findByText(t("显示的是缓存数据"));
  await userEvent.click(screen.getByRole("radio", { name: t("技能") }));
  await screen.findByRole("button", { name: /notes-kit/ });
  expect(screen.queryByText(t("显示的是缓存数据"))).toBeNull();
});

it("a failed listing with no copy still shows the error, not a cache label", async () => {
  const port = fixture();
  port.marketList = vi.fn().mockRejectedValue(new Error("boom"));
  render(<Market port={port} onInstalled={() => {}} />);
  expect(await screen.findByText(t("无法读取社区市场"))).toBeTruthy();
  expect(screen.queryByText(t("显示的是缓存数据"))).toBeNull();
});

it("labels a cached detail and retry re-reads it", async () => {
  const port = fixture();
  (port.marketDetail as ReturnType<typeof vi.fn>).mockResolvedValueOnce({ package: pkg, pinned: true, cache });
  render(<Market port={port} onInstalled={() => {}} />);
  await userEvent.click(await screen.findByRole("button", { name: /notes-kit/ }));
  expect(await screen.findByText(t("显示的是缓存数据"))).toBeTruthy();
  await userEvent.click(screen.getByRole("button", { name: t("重试") }));
  await screen.findByRole("button", { name: t("查看将安装的内容") });
  expect(screen.queryByText(t("显示的是缓存数据"))).toBeNull();
  expect(port.marketDetail).toHaveBeenCalledTimes(2);
});

it("a cached entry cannot start an install and says why; a live one can", async () => {
  const port = fixture();
  (port.marketDetail as ReturnType<typeof vi.fn>).mockResolvedValueOnce({ package: pkg, pinned: true, cache });
  render(<Market port={port} onInstalled={() => {}} />);
  await userEvent.click(await screen.findByRole("button", { name: /notes-kit/ }));
  expect(await screen.findByText(t("当前显示的是缓存数据，安装需要连接市场"))).toBeTruthy();
  expect((screen.getByRole("button", { name: t("查看将安装的内容") }) as HTMLButtonElement).disabled).toBe(true);
  await userEvent.click(screen.getByRole("button", { name: t("重试") }));
  const live = await screen.findByRole("button", { name: t("查看将安装的内容") });
  expect((live as HTMLButtonElement).disabled).toBe(false);
  expect(screen.queryByText(t("当前显示的是缓存数据，安装需要连接市场"))).toBeNull();
});

it("an unpinned cached entry cannot be trusted-installed either", async () => {
  const port = fixture();
  (port.marketDetail as ReturnType<typeof vi.fn>).mockResolvedValueOnce({ package: pkg, pinned: false, cache });
  render(<Market port={port} onInstalled={() => {}} />);
  await userEvent.click(await screen.findByRole("button", { name: /notes-kit/ }));
  expect((await screen.findByRole("button", { name: t("信任并安装") }) as HTMLButtonElement).disabled).toBe(true);
});

it("retry asks for a refresh and the first read does not", async () => {
  const port = fixture();
  (port.marketList as ReturnType<typeof vi.fn>).mockResolvedValueOnce({ packages: [pkg], limit: 24, offset: 0, cache });
  render(<Market port={port} onInstalled={() => {}} />);
  await screen.findByText(t("显示的是缓存数据"));
  expect(port.marketList).toHaveBeenLastCalledWith(expect.not.objectContaining({ refresh: true }));
  await userEvent.click(screen.getByRole("button", { name: t("重试") }));
  await screen.findByRole("button", { name: /notes-kit/ });
  expect(port.marketList).toHaveBeenLastCalledWith(expect.objectContaining({ refresh: true }));
});
