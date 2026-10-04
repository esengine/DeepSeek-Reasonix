// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ThemeImport } from "./ThemeImport";
import { SseTheme } from "../port/sse_theme";
import { boot, STORAGE, t } from "../i18n";
import type { AgentPort } from "../port/port";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  localStorage.setItem(STORAGE, "zh");
  boot();
});

it.each(["zh", "en"])("refuses mixed selection and lets the user retry one ZIP with canonical feedback (%s)", async (lang) => {
  localStorage.setItem(STORAGE, lang);
  boot();
  const raw = new TextEncoder().encode("archive fixture");
  const zip = new File([raw], "dusk.zip");
  const read = vi.fn().mockResolvedValue(raw.buffer);
  Object.defineProperty(zip, "arrayBuffer", { value: read });
  const fetch = vi.fn().mockResolvedValue(Response.json({
    pack: { id: "returned-id", name: "Dusk", tokens: {} }, ignored: ["notes.txt"],
  }));
  vi.stubGlobal("fetch", fetch);
  const onImported = vi.fn();
  const onUse = vi.fn();
  render(<ThemeImport port={new SseTheme() as unknown as AgentPort} empty={false} onImported={onImported} onUse={onUse} />);
  const input = document.querySelector<HTMLInputElement>('input[data-action="theme.import"]')!;
  fireEvent.change(input, { target: { files: [zip, new File(["image fixture"], "preview.png")] } });
  const message = lang === "en"
    ? "Choose a single .zip on its own, or select a theme folder’s theme.json and images without a ZIP."
    : "请单独选择一个 .zip，或选择不含压缩包的 theme.json 和图片。";
  expect((await screen.findByRole("alert")).textContent).toBe(message);
  expect(read).not.toHaveBeenCalled();
  expect(fetch).not.toHaveBeenCalled();
  expect(onImported).not.toHaveBeenCalled();
  expect(screen.queryByRole("button", { name: t("立即使用") })).toBeNull();
  expect(screen.getByRole<HTMLButtonElement>("button", { name: t("导入主题…") }).disabled).toBe(false);
  expect(input.value).toBe("");
  fireEvent.change(input, { target: { files: [zip] } });
  const receipt = t("已导入「{name}」。", { name: "Dusk" }) + " " + t("未读取：{names}", { names: "notes.txt" });
  expect(await screen.findByText(receipt)).toBeTruthy();
  expect(screen.queryByRole("alert")).toBeNull();
  expect(fetch).toHaveBeenCalledTimes(1);
  expect(onImported).toHaveBeenCalledTimes(1);
  await userEvent.click(screen.getByRole("button", { name: t("立即使用") }));
  expect(onUse).toHaveBeenCalledExactlyOnceWith("returned-id");
});
