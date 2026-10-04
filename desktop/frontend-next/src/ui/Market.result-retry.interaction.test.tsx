// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Market } from "./Market";
import { MockPort } from "../port/mock";
import { boot, STORAGE, t } from "../i18n";
import type { AgentPort, MarketDetail, MarketPackage, MarketPlan } from "../port/port";

afterEach(() => { cleanup(); localStorage.setItem(STORAGE, "zh"); boot(); });

const pkg: MarketPackage = {
  slug: "demo/notes-kit", kind: "plugin", handle: "demo", name: "notes-kit", summary: "Fixture",
  description: "", homepage: "", repoUrl: "", tags: [], latestVersion: "1.0.0", installCount: 0,
  starCount: 0, upCount: 0, downCount: 0, approvalRate: null, score: 0, verified: false, status: "active", updatedAt: "",
};
const plan = (id: string, unreviewed = false): MarketPlan => ({
  ok: true, status: "planned", applied: false, slug: pkg.slug, version: id === "consumed" ? "1.0.0" : "2.0.0", planId: id,
  contentDigest: `sha256:${(id === "consumed" ? "a" : "b").repeat(64)}`, unreviewed,
  actions: ["notes", "checks"].map((name) => ({ kind: "skill", name, action: "copy_skill", status: "planned", riskLevel: "low" })),
});
function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
async function confirm() {
  await userEvent.click(await screen.findByRole("checkbox", { name: t("我已看过这 {n} 个技能，全部安装", { n: 2 }) }));
  await userEvent.click(screen.getByRole("button", { name: t("安装") }));
}
async function fixture(unreviewed = false, applied = false, partial = false) {
  const port = new MockPort() as unknown as AgentPort;
  const original = plan("consumed", unreviewed);
  const fresh = plan("fresh", unreviewed);
  if (partial) fresh.version = original.version;
  const detail: MarketDetail = { package: pkg, pinned: !unreviewed };
  port.marketList = vi.fn().mockResolvedValue({ packages: [pkg], limit: 24, offset: 0 });
  port.marketMyVote = vi.fn().mockResolvedValue({ signedIn: false, value: 0 });
  const read = vi.fn().mockResolvedValue(detail);
  const preview = vi.fn().mockResolvedValueOnce(original).mockResolvedValue(fresh);
  const install = vi.fn().mockResolvedValueOnce({ ...original, ok: false, applied, status: partial ? "partial" : "failed",
    actions: original.actions!.map((action, i) => partial && i === 0 ? { ...action, status: "done" } : { ...action, status: "failed", error: "Target unavailable", next: "Correct the target" }),
  }).mockResolvedValue({ ...fresh, status: "done", applied: true, actions: fresh.actions!.map((a) => ({ ...a, status: "done" })) });
  port.marketDetail = read; port.planMarket = preview; port.installMarket = install;
  const onInstalled = vi.fn();
  const view = render(<Market port={port} onInstalled={onInstalled} />);
  await userEvent.click(await screen.findByRole("button", { name: /notes-kit/ }));
  await userEvent.click(await screen.findByRole("button", { name: t(unreviewed ? "信任并安装" : "查看将安装的内容") }));
  await confirm();
  await screen.findAllByRole("alert");
  return { port, original, fresh, detail, read, preview, install, onInstalled, view };
}

it.each(["zh", "en"].flatMap((lang) => [false, true].map((unreviewed) => ({ lang, unreviewed }))))("reads current details before retrying a failed market install ($lang, unreviewed=$unreviewed)", async ({ lang, unreviewed }) => {
  localStorage.setItem(STORAGE, lang); boot();
  const f = await fixture(unreviewed);
  const read = deferred<MarketDetail>();
  f.read.mockImplementationOnce(() => read.promise);
  const retry = screen.getByRole("button", { name: t("重试") });
  retry.focus();
  await userEvent.keyboard("{Enter}");
  expect(f.read).toHaveBeenCalledTimes(2);
  expect(f.read).toHaveBeenLastCalledWith(pkg.slug);
  expect(f.preview).toHaveBeenCalledTimes(1);
  expect(f.install).toHaveBeenCalledTimes(1);
  expect(screen.queryByRole("button", { name: t("重试") })).toBeNull();
  expect(screen.queryByRole("button", { name: t("安装") })).toBeNull();
  expect(screen.queryByRole("checkbox", { name: t("我已看过这 {n} 个技能，全部安装", { n: 2 }) })).toBeNull();
  expect(screen.getByRole("status").textContent).toBe(t("正在读取…"));
  expect(document.activeElement).toBe(screen.getByRole("region", { name: pkg.slug }));
  await act(async () => read.resolve({ ...f.detail, package: { ...pkg, latestVersion: "2.0.0" } }));
  await userEvent.click(screen.getByRole("button", { name: t(unreviewed ? "信任并安装" : "查看将安装的内容") }));
  expect(f.preview).toHaveBeenLastCalledWith({ slug: pkg.slug, replace: false, ...(unreviewed ? { trust: true } : {}) });
  expect(screen.getByRole<HTMLInputElement>("checkbox", { name: t("我已看过这 {n} 个技能，全部安装", { n: 2 }) }).checked).toBe(false);
  expect(screen.getByRole<HTMLButtonElement>("button", { name: t("安装") }).disabled).toBe(true);
  await confirm();
  expect(f.install).toHaveBeenLastCalledWith({ slug: pkg.slug, version: "2.0.0", planId: "fresh", replace: false,
    ...(unreviewed ? { trust: true, digest: f.fresh.contentDigest } : {}),
  });
  await screen.findByRole("button", { name: t("返回列表") });
  expect(screen.queryByRole("button", { name: t("重试") })).toBeNull();
  expect(f.onInstalled).toHaveBeenCalledOnce();
});

it("retains a partial result until retry and uses the refreshed installed state for replacement", async () => {
  const f = await fixture(false, true);
  expect(f.onInstalled).toHaveBeenCalledOnce();
  expect(screen.getAllByRole("alert").filter((alert) => alert.textContent?.includes("Target unavailable"))).toHaveLength(2);
  expect(f.read).toHaveBeenCalledOnce();
  f.read.mockResolvedValueOnce({ ...f.detail, installed: { version: "1.0.0", contentHash: f.original.contentDigest! }, package: { ...pkg, latestVersion: "2.0.0" } });
  await userEvent.click(screen.getByRole("button", { name: t("重试") }));
  await userEvent.click(await screen.findByRole("button", { name: t("查看更新内容") }));
  expect(f.preview).toHaveBeenLastCalledWith({ slug: pkg.slug, replace: true });
  await confirm();
  expect(f.install).toHaveBeenLastCalledWith({ slug: pkg.slug, version: "2.0.0", planId: "fresh", replace: true });
  await waitFor(() => expect(f.onInstalled).toHaveBeenCalledTimes(2));
});

it("recovers a failed detail refresh without replaying the consumed installation", async () => {
  const f = await fixture();
  f.read.mockRejectedValueOnce(new Error("current details unavailable"));
  await userEvent.click(screen.getByRole("button", { name: t("重试") }));
  expect((await screen.findByRole("alert")).textContent).toContain("current details unavailable");
  expect(f.install).toHaveBeenCalledOnce();
  expect(f.preview).toHaveBeenCalledOnce();
  await userEvent.click(screen.getByRole("button", { name: t("重试") }));
  await screen.findByRole("button", { name: t("查看将安装的内容") });
  expect(f.read).toHaveBeenCalledTimes(3);
  expect(f.install).toHaveBeenCalledOnce();
});

it.each(["zh", "en"].flatMap((lang) => [false, true].map((unreviewed) => ({ lang, unreviewed }))))("keeps failed items reachable when a partial install records the current version ($lang, unreviewed=$unreviewed)", async ({ lang, unreviewed }) => {
  localStorage.setItem(STORAGE, lang); boot();
  const f = await fixture(unreviewed, true, true);
  f.read.mockResolvedValueOnce({ ...f.detail, installed: { version: "1.0.0", contentHash: f.original.contentDigest! } });
  f.install.mockResolvedValueOnce({ ...f.fresh, ok: false, applied: true, status: "partial",
    actions: f.fresh.actions!.map((a, i) => i === 0 ? { ...a, status: "failed", error: "Already installed", next: "Keep the existing skill" } : { ...a, status: "done" }),
  });
  screen.getByRole("button", { name: t("重试") }).focus();
  await userEvent.keyboard("{Enter}");
  const preview = await screen.findByRole("button", { name: t(unreviewed ? "信任并安装" : "查看将安装的内容") });
  expect(screen.getByRole("alert").textContent).toContain("Target unavailable");
  expect(screen.getByRole("alert").textContent).toContain("checks");
  expect(screen.getByRole("alert").textContent).toContain("Correct the target");
  expect(screen.queryByText(t("已安装 {version}", { version: "1.0.0" }))).toBeNull();
  expect(document.activeElement).toBe(screen.getByRole("region", { name: pkg.slug }));
  expect(f.install).toHaveBeenCalledOnce();
  await userEvent.click(preview);
  expect(f.preview).toHaveBeenLastCalledWith({ slug: pkg.slug, replace: false, ...(unreviewed ? { trust: true } : {}) });
  expect(screen.getByRole<HTMLInputElement>("checkbox", { name: t("我已看过这 {n} 个技能，全部安装", { n: 2 }) }).checked).toBe(false);
  await confirm();
  expect(f.install).toHaveBeenLastCalledWith({ slug: pkg.slug, version: "1.0.0", planId: "fresh", replace: false,
    ...(unreviewed ? { trust: true, digest: f.fresh.contentDigest } : {}),
  });
  await waitFor(() => expect(f.onInstalled).toHaveBeenCalledTimes(2));
  expect(screen.getByRole("alert").textContent).toContain("Already installed");
  expect(screen.queryByText("Target unavailable")).toBeNull();
  expect(screen.getByRole("button", { name: t("重试") })).toBeTruthy();
});

it("keeps a partial result through a failed refresh and a cancelled new preview", async () => {
  const f = await fixture(false, true, true);
  f.read.mockRejectedValueOnce(new Error("offline after partial"));
  f.read.mockResolvedValueOnce({ ...f.detail, installed: { version: "1.0.0", contentHash: f.original.contentDigest! } });
  await userEvent.click(screen.getByRole("button", { name: t("重试") }));
  expect((await screen.findByRole("alert")).textContent).toContain("offline after partial");
  await userEvent.click(screen.getByRole("button", { name: t("重试") }));
  await userEvent.click(await screen.findByRole("button", { name: t("查看将安装的内容") }));
  await userEvent.click(screen.getByRole("button", { name: t("返回") }));
  expect(screen.getByRole("alert").textContent).toContain("Target unavailable");
  await userEvent.click(screen.getByRole("button", { name: t("查看将安装的内容") }));
  expect(screen.getByRole<HTMLInputElement>("checkbox", { name: t("我已看过这 {n} 个技能，全部安装", { n: 2 }) }).checked).toBe(false);
  expect(f.install).toHaveBeenCalledOnce();
});

it("does not carry a partial retry to a new host's complete installation", async () => {
  const f = await fixture(false, true, true);
  f.read.mockResolvedValueOnce({ ...f.detail, installed: { version: "1.0.0", contentHash: f.original.contentDigest! } });
  await userEvent.click(screen.getByRole("button", { name: t("重试") }));
  await screen.findByRole("button", { name: t("查看将安装的内容") });
  const next = new MockPort() as unknown as AgentPort;
  next.marketDetail = vi.fn().mockResolvedValue({ ...f.detail, installed: { version: "1.0.0", contentHash: f.original.contentDigest! } });
  next.marketMyVote = vi.fn().mockResolvedValue({ signedIn: false, value: 0 });
  next.planMarket = vi.fn();
  f.view.rerender(<Market port={next} onInstalled={f.onInstalled} />);
  await screen.findByText(t("已安装 {version}", { version: "1.0.0" }));
  expect(screen.queryByRole("alert")).toBeNull();
  expect(screen.queryByRole("button", { name: t("查看将安装的内容") })).toBeNull();
  expect(next.planMarket).not.toHaveBeenCalled();
  expect(f.install).toHaveBeenCalledOnce();
});
