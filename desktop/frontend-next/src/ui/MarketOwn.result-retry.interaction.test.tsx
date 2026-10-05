// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { OwnInstall } from "./MarketOwn";
import { boot, STORAGE, t } from "../i18n";
import type { AgentPort, MarketPackage, MarketPlan } from "../port/port";

afterEach(cleanup);

const pkg = { slug: "demo/notes-kit", kind: "skill", latestVersion: "1.0.0", status: "private" } as MarketPackage;
const preview = (id: string, version: string): MarketPlan => ({
  ok: true, status: "planned", applied: false, slug: pkg.slug, version, planId: id,
  contentDigest: `sha256:${(id === "consumed" ? "a" : "b").repeat(64)}`, unreviewed: true,
  actions: ["notes", "checks"].map((name) => ({ kind: "skill", name, action: "copy_skill", status: "planned", riskLevel: "low" })),
});
const original = preview("consumed", "1.0.0");
const fresh = preview("fresh", "2.0.0");
const failed = (applied: boolean): MarketPlan => ({
  ...original, ok: false, status: "failed", applied,
  actions: original.actions!.map((action) => ({ ...action, status: "failed", error: "The target is unavailable", next: "Correct the target and preview again" })),
});
const successful: MarketPlan = { ...fresh, status: "done", applied: true, actions: fresh.actions!.map((action) => ({ ...action, status: "done" })) };

function language(lang: string) {
  localStorage.setItem(STORAGE, lang);
  boot();
}

async function confirm() {
  await userEvent.click(await screen.findByRole("checkbox"));
  await userEvent.click(screen.getByRole("button", { name: t("安装") }));
}

it.each(["zh", "en"].flatMap((lang) => [false, true].map((applied) => ({ lang, applied }))))("re-previews a failed author install with fresh version, digest and consent ($lang, applied=$applied)", async ({ lang, applied }) => {
  language(lang);
  let finish!: (plan: MarketPlan) => void;
  const planning = vi.fn().mockResolvedValueOnce(original).mockImplementationOnce(() => new Promise<MarketPlan>((resolve) => { finish = resolve; }));
  const installing = vi.fn().mockResolvedValueOnce(failed(applied)).mockResolvedValueOnce(successful);
  const onInstalled = vi.fn();
  const onApplying = vi.fn();
  const onBack = vi.fn();
  const port = { planOwnMarket: planning, installOwnMarket: installing } as unknown as AgentPort;
  render(<OwnInstall port={port} pkg={pkg} onBack={onBack} onInstalled={onInstalled} onApplying={onApplying} />);
  await confirm();
  expect((await screen.findAllByRole("alert"))[0].textContent).toContain("The target is unavailable");
  await userEvent.click(screen.getByRole("button", { name: t("重试") }));
  expect(planning).toHaveBeenCalledTimes(2);
  expect(planning).toHaveBeenLastCalledWith({ slug: pkg.slug, replace: false });
  expect(screen.queryByRole("checkbox")).toBeNull();
  expect(screen.queryByRole("button", { name: t("安装") })).toBeNull();
  expect(screen.queryByRole("button", { name: t("重试") })).toBeNull();
  expect(installing).toHaveBeenCalledTimes(1);
  await act(async () => finish(fresh));
  const seen = screen.getByRole<HTMLInputElement>("checkbox");
  expect(seen.checked).toBe(false);
  expect(screen.getByRole<HTMLButtonElement>("button", { name: t("安装") }).disabled).toBe(true);
  expect(screen.getByText(t("内容摘要：{digest}", { digest: fresh.contentDigest! }))).toBeTruthy();
  await userEvent.click(seen);
  await userEvent.click(screen.getByRole("button", { name: t("安装") }));
  expect(installing).toHaveBeenLastCalledWith({ slug: pkg.slug, version: "2.0.0", planId: "fresh", replace: false, digest: fresh.contentDigest });
  expect(onInstalled).toHaveBeenCalledTimes(applied ? 2 : 1);
  expect(onApplying.mock.calls.filter(([value]) => value)).toHaveLength(2);
  expect(onBack).not.toHaveBeenCalled();
  expect(screen.queryByRole("button", { name: t("重试") })).toBeNull();
});

it.each(["zh", "en"])("recovers a new preview failure without reapplying the consumed plan (%s)", async (lang) => {
  language(lang);
  const planning = vi.fn().mockResolvedValueOnce(original).mockRejectedValueOnce(new Error("source unavailable")).mockResolvedValueOnce(fresh);
  const installing = vi.fn().mockResolvedValueOnce(failed(true)).mockResolvedValueOnce(successful);
  const port = { planOwnMarket: planning, installOwnMarket: installing } as unknown as AgentPort;
  render(<OwnInstall port={port} pkg={pkg} onBack={() => {}} onInstalled={() => {}} />);
  await confirm();
  await userEvent.click(await screen.findByRole("button", { name: t("重试") }));
  expect(await screen.findByText("source unavailable")).toBeTruthy();
  expect(installing).toHaveBeenCalledTimes(1);
  await userEvent.click(screen.getByRole("button", { name: t("重试") }));
  expect(await screen.findByRole("checkbox")).toBeTruthy();
  expect(planning).toHaveBeenCalledTimes(3);
  expect(installing).toHaveBeenCalledTimes(1);
  await confirm();
  expect(installing).toHaveBeenLastCalledWith({ slug: pkg.slug, version: "2.0.0", planId: "fresh", replace: false, digest: fresh.contentDigest });
});
