// @vitest-environment jsdom
import { StrictMode, useState } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { AddProvider } from "./AddProvider";
import { SsePort } from "../port/sse";
import { boot, STORAGE } from "../i18n";
import type { ProviderDraft } from "../port/port";

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((yes) => { resolve = yes; });
  return { promise, resolve };
}

function response(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });
}

function Host({ port, onDone, onCancel }: { port: SsePort; onDone: () => void; onCancel: () => void }) {
  const [open, setOpen] = useState(true);
  return open ? <AddProvider port={port} taken={[]} known={[]}
    onDone={() => { onDone(); setOpen(false); }} onCancel={onCancel} /> : <p>Source added</p>;
}

function draw(strict: boolean, pendingSave?: Promise<Response>, pendingProbe?: Promise<Response>) {
  localStorage.setItem(STORAGE, "zh");
  boot();
  const sent: { path: string; body: unknown }[] = [];
  vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const path = new URL(String(input)).pathname;
    if (path === "/providers/protocols") return Promise.resolve(response([
      { kind: "openai", discovery: "openai", serverWebSearch: false, reasoningParams: true },
    ]));
    sent.push({ path, body: JSON.parse(String(init?.body)) });
    if (path === "/providers" && pendingSave) return pendingSave;
    if (path === "/providers/probe" && pendingProbe) return pendingProbe;
    throw new Error(`Unexpected request ${path}`);
  }));
  const onDone = vi.fn();
  const onCancel = vi.fn();
  const host = <Host port={new SsePort("http://kernel.example")} onDone={onDone} onCancel={onCancel} />;
  return { ...render(strict ? <StrictMode>{host}</StrictMode> : host), sent, onDone, onCancel };
}

async function fillDraft(container: HTMLElement) {
  const user = userEvent.setup();
  await user.type(screen.getByLabelText("来源名称"), "relay");
  await user.type(screen.getByLabelText("接口地址"), "https://relay.example/v1");
  await user.type(screen.getByLabelText("API Key"), "test-key");
  await user.type(screen.getByLabelText("上下文窗口"), "32000");
  await user.type(screen.getByLabelText("最大输出"), "4000");
  await user.type(screen.getByRole("searchbox", { name: "搜索或添加模型" }), "alpha{enter}beta{enter}");
  await user.click(container.querySelector("summary")!);
  await user.selectOptions(screen.getByText("思考协议").closest("label")!.querySelector("select")!, "openai");
  await user.type(screen.getByText("额外请求头").closest("label")!.querySelector("textarea")!, "X-Site: studio");
  await user.click(screen.getByText("额外请求体").closest("label")!.querySelector("textarea")!);
  await user.paste('{"temperature":0.2}');
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
  await user.selectOptions(screen.getByText("思考协议").closest("label")!.querySelector("select")!, "anthropic");
  await user.click(screen.getByRole("checkbox", { name: "选用 beta" }));
  await user.type(screen.getByRole("searchbox"), "manual-model{enter}");
  await user.click(screen.getByRole("switch", { name: "发送思考控制" }));
  await user.click(screen.getAllByRole("button", { name: "取消" })[0]);
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  localStorage.setItem(STORAGE, "zh");
  boot();
});

it.each([false, true].flatMap((strict) => ["success", "failure", "name"].map((outcome) => ({ strict, outcome }))))(
  "holds the submitted addition until its $outcome receipt (StrictMode=$strict)",
  async ({ strict, outcome }) => {
    const pending = deferred<Response>();
    const { container, sent, onDone, onCancel } = draw(strict, pending.promise);
    await fillDraft(container);
    const before = snapshot(container);
    await userEvent.click(screen.getByRole("button", { name: "添加来源" }));
    expect(sent).toHaveLength(1);
    const draft = sent[0].body as ProviderDraft;
    await changeDraft(container);
    const during = snapshot(container);
    const locked = [...container.querySelectorAll("input, select, textarea, button")].every((control) => control.matches(":disabled"));
    const betaHeld = screen.getByRole("checkbox", { name: "选用 beta" }).getAttribute("aria-checked");
    await act(async () => pending.resolve(response(outcome === "success" ? {} : {
      code: outcome === "name" ? "provider.name_taken" : "provider.endpoint_required",
      error: "controlled producer refusal", params: { name: "relay" },
    }, outcome === "success" ? 200 : 409)));
    expect(during).toEqual(before);
    expect(locked).toBe(true);
    expect(betaHeld).toBe("true");
    expect(sent).toHaveLength(1);
    expect(sent[0].path).toBe("/providers");
    expect(draft).toMatchObject({ name: "relay", baseUrl: "https://relay.example/v1", apiKey: "test-key",
      kind: "openai", models: ["alpha", "beta"], default: "alpha", contextWindow: 32000,
      maxOutputTokens: 4000, reasoningProtocol: "openai", headers: { "X-Site": "studio" }, extraBody: { temperature: 0.2 } });
    expect(onCancel).not.toHaveBeenCalled();
    if (outcome === "success") {
      expect(onDone).toHaveBeenCalledTimes(1);
      expect(screen.getByText("Source added")).toBeTruthy();
    } else {
      expect(onDone).not.toHaveBeenCalled();
      expect(snapshot(container)).toEqual(before);
      expect(screen.getByLabelText<HTMLInputElement>("来源名称").getAttribute("aria-invalid")).toBe(outcome === "name" ? "true" : null);
      expect(screen.queryByText("无法保存") !== null).toBe(outcome === "failure");
      if (outcome === "failure") {
        const message = screen.getByRole("alert");
        expect(message.getAttribute("data-lvl")).toBe("err");
        expect(message.textContent).toContain("无法保存");
      }
      const win = screen.getByLabelText<HTMLInputElement>("上下文窗口");
      expect(win.matches(":disabled")).toBe(false);
      await userEvent.clear(win);
      await userEvent.type(win, "64000");
      expect(win.value).toBe("64000");
      expect(screen.getByRole("button", { name: "添加来源" }).matches(":disabled")).toBe(false);
    }
  },
);

it.each([false, true])("preserves limit edits during a catalog probe (StrictMode=%s)", async (strict) => {
  const pending = deferred<Response>();
  const { container, sent, onDone } = draw(strict, undefined, pending.promise);
  await fillDraft(container);
  await userEvent.click(screen.getByRole("button", { name: "验证连接并读取" }));
  const win = screen.getByLabelText<HTMLInputElement>("上下文窗口");
  await userEvent.clear(win);
  await userEvent.type(win, "64000");
  await act(async () => pending.resolve(response({ kind: "openai", kinds: ["openai"],
    baseUrl: "https://relay.example/v1", models: ["remote"], default: "remote", authHeader: false,
    efforts: [], effort: "", vision: [], ambiguous: false, noProxy: false })));
  expect(win.value).toBe("64000");
  expect(screen.getByRole("checkbox", { name: "选用 alpha" }).getAttribute("aria-checked")).toBe("true");
  expect(screen.getByRole("checkbox", { name: "选用 beta" }).getAttribute("aria-checked")).toBe("true");
  expect(sent).toEqual([{ path: "/providers/probe", body: { baseUrl: "https://relay.example/v1", apiKey: "test-key" } }]);
  expect(onDone).not.toHaveBeenCalled();
});

it.each([false, true])("keeps normal draft editing and cancel available (StrictMode=%s)", async (strict) => {
  const { container, sent, onCancel, onDone } = draw(strict);
  await fillDraft(container);
  await changeDraft(container);
  expect(screen.getByLabelText<HTMLInputElement>("上下文窗口").value).toBe("320009");
  expect(screen.getByRole("checkbox", { name: "选用 beta" }).getAttribute("aria-checked")).toBe("false");
  expect(screen.getByRole("checkbox", { name: "选用 manual-model" }).getAttribute("aria-checked")).toBe("true");
  expect(onCancel).toHaveBeenCalledTimes(1);
  expect(onDone).not.toHaveBeenCalled();
  expect(sent).toHaveLength(0);
});
