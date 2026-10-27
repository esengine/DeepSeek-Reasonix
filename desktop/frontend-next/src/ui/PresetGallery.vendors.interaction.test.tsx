// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import type { ProviderPreset } from "../port/port";
import { PresetGallery } from "./PresetGallery";

afterEach(cleanup);

const p = (id: string, label: string, vendor: string, baseUrl: string): ProviderPreset => ({ id, label, kind: "openai", baseUrl, vendor });
const presets = [
  p("deepseek-a", "DeepSeek API", "deepseek", "https://api.deepseek.com"),
  p("kimi-cn", "Kimi CN", "kimi", "https://api.moonshot.cn/v1"),
  p("kimi-global", "Kimi Global", "kimi", "https://api.moonshot.ai/v1"),
  p("mystery", "Mystery Relay", "", "https://mystery.example/v1"),
];

async function open(onPick = vi.fn()) {
  render(<PresetGallery load={async () => presets} onPick={onPick} disabled={false} />);
  await userEvent.click(await screen.findByRole("button", { name: /从常用服务开始/ }));
  return onPick;
}

it("offers one chip per declared vendor and draws the vendor's mark on its cards", async () => {
  await open();
  const chips = screen.getByRole("group", { name: "按厂商筛选" });
  expect(within(chips).getAllByRole("button").map((b) => b.textContent)).toEqual(["全部", "DeepSeek", "Kimi"]);
  expect(document.querySelectorAll('.addp-preset [data-mark="kimi"]').length).toBe(2);
  expect(screen.getByRole("button", { name: /Mystery Relay/ }).querySelector(".vendor-mark")).toBeNull();
});

it("narrows the cards to the chosen vendor and back, without losing what the search typed", async () => {
  const onPick = await open();
  await userEvent.type(screen.getByRole("searchbox", { name: "搜索常用服务" }), "kimi");
  await userEvent.click(within(screen.getByRole("group", { name: "按厂商筛选" })).getByRole("button", { name: "Kimi" }));
  expect(screen.queryByRole("button", { name: /DeepSeek API/ })).toBeNull();
  expect(screen.getByRole("button", { name: /Kimi Global/ })).toBeTruthy();
  await userEvent.click(within(screen.getByRole("group", { name: "按厂商筛选" })).getByRole("button", { name: "全部" }));
  expect((screen.getByRole("searchbox", { name: "搜索常用服务" }) as HTMLInputElement).value).toBe("kimi");
  await userEvent.click(screen.getByRole("button", { name: /Kimi CN/ }));
  expect(onPick).toHaveBeenCalledWith(presets[1]);
});
