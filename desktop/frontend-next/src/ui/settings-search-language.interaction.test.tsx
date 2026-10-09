// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Settings } from "./Settings";
import { SETTINGS, SECTION_NAME, SETTING_AT, settingMatches } from "./prefsnav";
import { boot, STORAGE, t } from "../i18n";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";
import type { AgentPort, SessionStatus } from "../port/port";

afterEach(() => { cleanup(); vi.restoreAllMocks(); localStorage.setItem(STORAGE, "zh"); boot(); });
const language = (lang: string) => { localStorage.setItem(STORAGE, lang); boot(); };
const block = (anchor: string) => document.querySelector<HTMLElement>(`[data-setting="${anchor}"]`);

function draw() {
  const port = new MockPort();
  const calls: string[] = [];
  const watched = new Proxy(port, {
    get: (own, key: string) => {
      const value = (own as unknown as Record<string, unknown>)[key];
      if (typeof value !== "function") return value;
      return (...args: unknown[]) => { calls.push(key); return value.apply(own, args); };
    },
  }) as unknown as AgentPort;
  const onClose = vi.fn();
  render(<Settings
    hub={new MockHub() as never} port={watched}
    status={{ preset: "balanced", toolApprovalMode: "ask" } as SessionStatus}
    theme="light" onTheme={() => {}} contrast="" onContrast={() => {}} weight="" onWeight={() => {}}
    look={{} as never} onLook={() => {}} reloadThemes={() => {}}
    onClose={onClose} onChanged={() => {}} onError={() => {}}
    account={null} accountUnread="" reloadAccount={() => {}}
  />);
  return { calls, onClose, find: () => screen.getByRole<HTMLInputElement>("textbox", { name: t("搜索设置") }) };
}

it.each(["market", "plugins", "mcp", "skills", "ext-runtime", "hooks"])(
  "finds the unmounted %s block by its displayed English title and lands on it",
  async (anchor) => {
    language("en");
    const entry = SETTING_AT(anchor)!;
    const { find, calls } = draw();
    await act(async () => {});
    expect(block(anchor)).toBeNull();
    const reads = [...calls];
    await userEvent.type(find(), `  ${t(entry.title).toUpperCase()}  `);
    const hit = screen.queryAllByRole<HTMLButtonElement>("option").find((item) => item.dataset.target === anchor);
    expect(hit).toBeDefined();
    expect(hit?.dataset.value).toBe(entry.section);
    expect(hit?.querySelector(".what")?.textContent).toBe(t(entry.title));
    expect(block(anchor)).toBeNull();
    expect(calls).toEqual(reads);
    if (anchor === "market") {
      hit!.focus();
      await userEvent.keyboard("{Enter}");
    } else await userEvent.click(hit!);
    await waitFor(() => expect(block(anchor)).not.toBeNull());
    expect(find().value).toBe("");
    if (entry.section === "ext") {
      expect(screen.getByRole("tab", { name: t(anchor === "market" ? "发现" : "已安装") }).getAttribute("aria-selected")).toBe("true");
    }
  },
);

it("finds extension blocks by the displayed section name without mounting them", async () => {
  language("en");
  const { find, calls, onClose } = draw();
  await act(async () => {});
  const reads = [...calls];
  await userEvent.type(find(), "Extension");
  const hits = screen.getAllByRole<HTMLButtonElement>("option");
  expect(hits.map((hit) => hit.dataset.target)).toEqual(SETTINGS.filter((entry) => entry.section === "ext").map((entry) => entry.anchor));
  expect(hits.every((hit) => hit.querySelector(".where")?.textContent === "Extension")).toBe(true);
  expect(calls).toEqual(reads);
  expect(block("plugins")).toBeNull();
  await userEvent.keyboard("{Escape}");
  expect(find().value).toBe("");
  expect(document.activeElement).toBe(find());
  expect(onClose).not.toHaveBeenCalled();
  await userEvent.type(find(), "not-a-setting");
  expect(screen.queryByRole("option")).toBeNull();
  expect(screen.getByText(t("没有匹配的设置"))).toBeTruthy();
  await userEvent.clear(find());
  expect(screen.queryByRole("listbox")).toBeNull();
});

it.each(["zh", "en"])("keeps every displayed %s catalogue title and section searchable", (lang) => {
  language(lang);
  const missingTitles = SETTINGS.filter((entry) => !settingMatches(entry, t(entry.title).toLowerCase())).map((entry) => entry.anchor);
  const missingSections = SETTINGS.filter((entry) => !settingMatches(entry, t(SECTION_NAME[entry.section]!).toLowerCase())).map((entry) => entry.anchor);
  expect(missingTitles).toEqual([]);
  expect(missingSections).toEqual([]);
});

it("retains canonical names and existing literal aliases in the English interface", () => {
  language("en");
  for (const entry of SETTINGS) {
    expect(settingMatches(entry, entry.title.toLowerCase()), entry.anchor).toBe(true);
    for (const alias of entry.keywords ?? []) expect(settingMatches(entry, alias.toLowerCase()), `${entry.anchor}: ${alias}`).toBe(true);
  }
  expect(settingMatches(SETTING_AT("plugins")!, "not-a-setting")).toBe(false);
});
