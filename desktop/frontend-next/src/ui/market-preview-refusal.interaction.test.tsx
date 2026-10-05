// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Market } from "./Market";
import { OwnInstall } from "./MarketOwn";
import { MockPort } from "../port/mock";
import type { AgentPort, MarketPlan } from "../port/port";

afterEach(cleanup);

it.each(["public", "own"] as const)("shows a blocked %s preview and retries before offering installation", async (mode) => {
  const port = new MockPort() as unknown as AgentPort;
  vi.spyOn(port, "marketMyVote").mockResolvedValue({ signedIn: false, value: 0 });
  const detail = await port.marketDetail("acme/review-kit");
  const pkg = { ...detail.package, kind: "plugin" as const, slug: "fixture/empty-claude", handle: "fixture", name: "empty-claude" };
  const valid = { ...await port.planMarket({ slug: "acme/review-kit" }), slug: pkg.slug };
  const blocked: MarketPlan = {
    ok: false, status: "blocked", applied: false, slug: pkg.slug, version: pkg.latestVersion,
    error: 'install_source: plugin has no compatible capabilities: plugin "empty-claude" has no Reasonix-compatible capabilities; skipped: []',
    next: "Choose a plugin that exports a supported skill, command, agent, hook, or MCP server.",
  };
  const read = vi.fn().mockResolvedValueOnce(blocked).mockResolvedValue(valid);
  const result = { ...valid, applied: true, status: "done", actions: valid.actions?.map((a) => ({ ...a, status: "done" })) };
  const install = vi.fn().mockResolvedValue(result);
  const onInstalled = vi.fn();
  if (mode === "public") {
    vi.spyOn(port, "marketList").mockResolvedValue({ packages: [pkg], limit: 24, offset: 0 });
    vi.spyOn(port, "marketDetail").mockResolvedValue({ ...detail, package: pkg });
    port.planMarket = read;
    port.installMarket = install;
    render(<Market port={port} onInstalled={onInstalled} />);
    await userEvent.click(await screen.findByRole("button", { name: /empty-claude/ }));
    await userEvent.click(await screen.findByRole("button", { name: "查看将安装的内容" }));
  } else {
    port.planOwnMarket = read;
    port.installOwnMarket = install;
    render(<OwnInstall port={port} pkg={pkg} onBack={() => {}} onInstalled={onInstalled} />);
    await act(async () => {});
  }
  expect(read).toHaveBeenCalledTimes(1);
  expect(screen.queryByText(blocked.error!)).not.toBeNull();
  expect(screen.getByRole("alert").textContent).toContain(blocked.error);
  expect(document.querySelector('[data-stage="confirm"]')).toBeNull();
  expect(screen.queryByRole("button", { name: "安装" })).toBeNull();
  expect(screen.queryByText("已按内容摘要核对：与审核时固定的版本一致。")).toBeNull();
  expect(install).not.toHaveBeenCalled();
  expect(onInstalled).not.toHaveBeenCalled();
  await userEvent.click(screen.getByRole("button", { name: mode === "public" ? "查看将安装的内容" : "重试" }));
  await screen.findByRole("checkbox", { name: "我已看过这 3 个技能，全部安装" });
  expect(read).toHaveBeenCalledTimes(2);
  expect(screen.queryByText(blocked.error!)).toBeNull();
  const button = screen.getByRole<HTMLButtonElement>("button", { name: "安装" });
  expect(button.disabled).toBe(true);
  await userEvent.click(screen.getByRole("checkbox", { name: "我已看过这 3 个技能，全部安装" }));
  await userEvent.click(button);
  expect(install).toHaveBeenCalledTimes(1);
  expect(install).toHaveBeenCalledWith(expect.objectContaining({ slug: pkg.slug, version: valid.version, planId: valid.planId }));
  expect(onInstalled).toHaveBeenCalledTimes(1);
});
