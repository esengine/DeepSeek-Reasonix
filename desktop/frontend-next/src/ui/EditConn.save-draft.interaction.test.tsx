// @vitest-environment jsdom
import { StrictMode, useState } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { EditConn } from "./EditConn";
import { SsePort } from "../port/sse";
import { boot, STORAGE } from "../i18n";
import type { ProviderEdit, ProviderEntry } from "../port/port";

const entry: ProviderEntry = {
  name: "relay", kind: "openai", baseUrl: "https://relay.example/v1",
  models: ["alpha", "beta"], default: "alpha", visionModels: ["alpha"],
  hasKey: true, inUse: false, preset: false, canSetVision: true,
  contextWindow: 32000, maxOutputTokens: 4000, idleTimeoutSeconds: 120,
  reasoningProtocol: "openai", supportedEfforts: ["low", "high"], defaultEffort: "low",
  modelEfforts: { alpha: { supportedEfforts: ["low", "high"], defaultEffort: "low" } },
  modelLimits: { alpha: { contextWindow: 48000, maxOutputTokens: 6000 } },
  headers: { "X-Site": "studio" }, extraBody: { temperature: 0.2 },
};

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((yes) => { resolve = yes; });
  return { promise, resolve };
}

function Host({ port, onDone, onRevert, onSaved }: { port: SsePort; onDone: () => void; onRevert: () => void; onSaved: () => void }) {
  const [busy, setBusy] = useState("");
  const [revision, setRevision] = useState(0);
  return <EditConn key={revision} entry={entry} port={port} busy={busy} setBusy={setBusy}
    onDone={() => { onDone(); setRevision((n) => n + 1); }} onRevert={() => { onRevert(); setRevision((n) => n + 1); }} onSaved={onSaved} />;
}

function draw(strict: boolean, port: SsePort) {
  localStorage.setItem(STORAGE, "zh");
  boot();
  const onDone = vi.fn();
  const onRevert = vi.fn();
  const onSaved = vi.fn();
  const host = <Host port={port} onDone={onDone} onRevert={onRevert} onSaved={onSaved} />;
  return { ...render(strict ? <StrictMode>{host}</StrictMode> : host), onDone, onRevert, onSaved };
}

function snapshot(container: HTMLElement) {
  return [...container.querySelectorAll<HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement>("input, select, textarea")]
    .map((field) => [field.getAttribute("aria-label") || field.closest("label")?.querySelector("span")?.textContent, field.value]);
}

async function changeDraft(container: HTMLElement) {
  const user = userEvent.setup();
  for (const field of container.querySelectorAll<HTMLInputElement | HTMLTextAreaElement>("input, textarea")) {
    if (field.type !== "password" && field.type !== "search") await user.type(field, "9");
  }
  await user.selectOptions(screen.getByLabelText<HTMLSelectElement>(/^思考参数/), "none");
  await user.click(screen.getByRole("button", { name: "将 beta 设为默认模型" }));
  await user.click(screen.getByRole("button", { name: "beta 的图片输入" }));
  await user.click(screen.getByRole("checkbox", { name: "选用 beta" }));
  await user.type(screen.getByRole("searchbox"), "manual-model{enter}");
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  localStorage.setItem(STORAGE, "zh");
  boot();
});

it.each([false, true].flatMap((strict) => ["success", "failure", "unapplied"].map((outcome) => ({ strict, outcome }))))(
  "keeps the whole submitted draft stable until a $outcome receipt (StrictMode=$strict)",
  async ({ strict, outcome }) => {
    const pending = deferred<Response>();
    const sent: ProviderEdit[] = [];
    vi.stubGlobal("fetch", vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
      sent.push(JSON.parse(String(init?.body)) as ProviderEdit);
      return pending.promise;
    }));
    const { container, onDone, onSaved } = draw(strict, new SsePort("http://kernel.example"));
    await userEvent.click(container.querySelector("summary")!);
    await userEvent.type(screen.getByLabelText("上下文窗口"), "1");
    const before = snapshot(container);
    await userEvent.click(screen.getByRole("button", { name: "保存" }));
    expect(sent).toHaveLength(1);
    expect(screen.getByRole("button", { name: "保存中…" }).matches(":disabled")).toBe(true);
    await changeDraft(container);
    const during = snapshot(container);
    const locked = [...container.querySelectorAll("input, select, textarea, button")].every((control) => control.matches(":disabled"));
    const defaultHeld = screen.getByRole("button", { name: "将 beta 设为默认模型" }).getAttribute("aria-pressed");
    const visionHeld = screen.getByRole("button", { name: "beta 的图片输入" }).getAttribute("aria-pressed");
    const pickedHeld = screen.getByRole("checkbox", { name: "选用 beta" }).getAttribute("aria-checked");
    await act(async () => pending.resolve(new Response(outcome === "success" ? "{}" : JSON.stringify({
      code: outcome === "unapplied" ? "provider.saved_while_running" : "provider.bad_idle_timeout",
      error: "controlled producer refusal", params: { min: 1, max: 32767 },
    }), { status: outcome === "success" ? 200 : 409, headers: { "content-type": "application/json" } })));
    expect(during).toEqual(before);
    expect(locked).toBe(true);
    expect(defaultHeld).toBe("false");
    expect(visionHeld).toBe("false");
    expect(pickedHeld).toBe("true");
    expect(sent[0]).toMatchObject({ models: ["alpha", "beta"], default: "alpha", vision: ["alpha"], contextWindow: 320001,
      maxOutputTokens: 4000, idleTimeoutSeconds: 120, reasoningProtocol: "openai", supportedEfforts: ["low", "high"],
      modelLimits: { alpha: { contextWindow: 48000, maxOutputTokens: 6000 } }, headers: { "X-Site": "studio" }, extraBody: { temperature: 0.2 } });
    expect(sent).toHaveLength(1);
    if (outcome === "success") {
      expect(onDone).toHaveBeenCalledTimes(1);
      expect(onSaved).not.toHaveBeenCalled();
    } else {
      expect(onDone).not.toHaveBeenCalled();
      expect(onSaved).toHaveBeenCalledTimes(outcome === "unapplied" ? 1 : 0);
      expect(snapshot(container)).toEqual(before);
      const win = screen.getByLabelText<HTMLInputElement>("上下文窗口");
      expect(win.matches(":disabled")).toBe(false);
      await userEvent.clear(win);
      await userEvent.type(win, "64000");
      expect(win.value).toBe("64000");
      expect(screen.getByRole("button", { name: "保存" }).matches(":disabled")).toBe(false);
    }
  },
);

it.each([false, true])("keeps normal edits and revert available before saving (StrictMode=%s)", async (strict) => {
  const fetch = vi.fn();
  vi.stubGlobal("fetch", fetch);
  const { container, onDone, onRevert } = draw(strict, new SsePort("http://kernel.example"));
  const win = screen.getByLabelText<HTMLInputElement>("上下文窗口");
  await userEvent.clear(win);
  await userEvent.type(win, "64000");
  expect(win.value).toBe("64000");
  await userEvent.click(container.querySelector("summary")!);
  await userEvent.selectOptions(screen.getByLabelText<HTMLSelectElement>(/^思考参数/), "none");
  expect(screen.getByLabelText<HTMLSelectElement>(/^思考参数/).value).toBe("none");
  await userEvent.click(screen.getByRole("button", { name: "还原" }));
  expect(onRevert).toHaveBeenCalledTimes(1);
  expect(onDone).not.toHaveBeenCalled();
  expect(fetch).not.toHaveBeenCalled();
});
