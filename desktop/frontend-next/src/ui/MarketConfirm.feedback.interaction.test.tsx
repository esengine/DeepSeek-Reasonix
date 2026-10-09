// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Market } from "./Market";
import { OwnInstall } from "./MarketOwn";
import { MockPort } from "../port/mock";
import { boot, STORAGE, t } from "../i18n";
import type { AgentPort, MarketPlan } from "../port/port";

afterEach(() => { cleanup(); localStorage.setItem(STORAGE, "zh"); boot(); });

const cases = ["zh", "en"].flatMap((lang) => ["listed", "own"].map((flow) => ({ lang, flow })));
function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
async function fixture(flow: string) {
  const port = new MockPort() as unknown as AgentPort;
  const pkg = flow === "own" ? (await port.myMarket()).find((p) => p.slug === "demo/ship-notes")!
    : (await port.marketList({})).packages.find((p) => p.name === "review-kit")!;
  const shown: MarketPlan = {
    ok: true, applied: false, status: "planned", slug: pkg.slug, version: "1.0.0", planId: "consented-plan",
    contentDigest: `sha256:${"a".repeat(64)}`, unreviewed: flow === "own",
    actions: ["notes", "checks"].map((name) => ({ kind: "skill", name, action: "copy_skill", status: "planned", riskLevel: "high" })),
  };
  const pending = deferred<MarketPlan>();
  const install = vi.spyOn(port, flow === "own" ? "installOwnMarket" : "installMarket").mockImplementationOnce(() => pending.promise);
  const onBack = vi.fn();
  const onInstalled = vi.fn();
  if (flow === "own") {
    vi.spyOn(port, "planOwnMarket").mockResolvedValue(shown);
    render(<OwnInstall port={port} pkg={pkg} onBack={onBack} onInstalled={onInstalled} />);
  } else {
    vi.spyOn(port, "marketList").mockResolvedValue({ packages: [pkg], limit: 24, offset: 0 });
    vi.spyOn(port, "marketDetail").mockResolvedValue({ package: pkg, pinned: true });
    vi.spyOn(port, "marketMyVote").mockResolvedValue({ signedIn: false, value: 0 });
    vi.spyOn(port, "planMarket").mockResolvedValue(shown);
    render(<Market port={port} onInstalled={onInstalled} />);
    await userEvent.click(await screen.findByRole("button", { name: /review-kit/ }));
    await userEvent.click(await screen.findByRole("button", { name: t("查看将安装的内容") }));
  }
  const consent = await screen.findByRole<HTMLInputElement>("checkbox", { name: t("我已看过这 {n} 个技能，全部安装", { n: 2 }) });
  const panel = consent.closest('[data-stage="confirm"]')!;
  const success: MarketPlan = { ...shown, applied: true, status: "done", actions: shown.actions!.map((a) => ({ ...a, status: "done" })) };
  return { consent, panel, shown, success, pending, install, onBack, onInstalled };
}

it.each(cases)("exposes the confirmation's pending lifetime ($lang, $flow)", async ({ lang, flow }) => {
  localStorage.setItem(STORAGE, lang); boot();
  const f = await fixture(flow);
  expect(f.panel.getAttribute("aria-busy")).toBe("false");
  await userEvent.click(f.consent);
  await userEvent.click(screen.getByRole("button", { name: t("安装") }));
  expect(f.panel.getAttribute("aria-busy")).toBe("true");
  await userEvent.click(screen.getByRole("button", { name: t("返回") }));
  expect(f.onBack).not.toHaveBeenCalled();
  expect(document.querySelector('[data-stage="confirm"]')).toBe(f.panel);
  expect(f.install).toHaveBeenCalledOnce();
  await act(async () => f.pending.resolve(f.success));
  expect(document.querySelector('[data-stage="confirm"]')).toBeNull();
  expect(f.onInstalled).toHaveBeenCalledOnce();
});

it.each(cases)("locks the acknowledged skill set during a deferred installation ($lang, $flow)", async ({ lang, flow }) => {
  localStorage.setItem(STORAGE, lang); boot();
  const f = await fixture(flow);
  expect(f.consent.disabled).toBe(false);
  expect(screen.getByRole<HTMLButtonElement>("button", { name: t("安装") }).disabled).toBe(true);
  await userEvent.click(f.consent);
  await userEvent.click(screen.getByRole("button", { name: t("安装") }));
  expect(f.consent.disabled).toBe(true);
  await userEvent.click(f.consent);
  expect(f.consent.checked).toBe(true);
  await userEvent.click(screen.getByRole("button", { name: t("安装中…") }));
  expect(f.install).toHaveBeenCalledOnce();
  await act(async () => f.pending.resolve(f.success));
  expect(f.onInstalled).toHaveBeenCalledOnce();
});

it.each(cases)("announces a thrown install failure and clears it for retry ($lang, $flow)", async ({ lang, flow }) => {
  localStorage.setItem(STORAGE, lang); boot();
  const f = await fixture(flow);
  await userEvent.click(f.consent);
  await userEvent.click(screen.getByRole("button", { name: t("安装") }));
  await act(async () => f.pending.reject(new Error("installation unavailable")));
  expect(screen.getByRole("alert").textContent).toBe("installation unavailable");
  expect(f.panel.getAttribute("aria-busy")).toBe("false");
  expect(f.consent.disabled).toBe(false);
  expect(f.onInstalled).not.toHaveBeenCalled();
  const retry = deferred<MarketPlan>();
  f.install.mockImplementationOnce(() => retry.promise);
  await userEvent.click(screen.getByRole("button", { name: t("安装") }));
  expect(screen.queryByRole("alert")).toBeNull();
  expect(f.panel.getAttribute("aria-busy")).toBe("true");
  expect(f.install).toHaveBeenCalledTimes(2);
  expect(f.install.mock.calls[1]).toEqual(f.install.mock.calls[0]);
  await act(async () => retry.resolve(f.success));
  expect(f.onInstalled).toHaveBeenCalledOnce();
});
