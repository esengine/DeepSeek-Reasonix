// @vitest-environment jsdom
import { StrictMode } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Backup } from "./Backup";
import { SsePort } from "../port/sse";
import { boot, STORAGE } from "../i18n";
import type { BackupCatalog, BackupCreateRequest, BackupEntry } from "../port/port";

const entry: BackupEntry = {
  id: "old/backup", label: "laptop", format: 1, appVersion: "2.29.0", platform: "darwin/arm64",
  categories: ["settings"], ciphertextBytes: 2048, createdAt: "2026-10-01T00:00:00Z",
};
const catalog: BackupCatalog = {
  categories: [
    { id: "settings", defaultOn: true, consent: false },
    { id: "extensions", defaultOn: true, consent: false },
    { id: "memory", defaultOn: true, consent: false },
    { id: "automation", defaultOn: true, consent: true },
    { id: "secrets", defaultOn: false, consent: true },
  ],
  backups: [entry], limits: { maxCount: 10, maxBytes: 1048576 }, minPassphrase: 10,
};

function response(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((yes) => { resolve = yes; });
  return { promise, resolve };
}

function draw(strict: boolean) {
  localStorage.setItem(STORAGE, "zh");
  boot();
  let removed = false;
  const reload = deferred<Response>();
  const calls: { path: string; method: string; body?: unknown }[] = [];
  vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const path = new URL(String(input)).pathname;
    const method = init?.method ?? "GET";
    calls.push({ path, method, body: init?.body ? JSON.parse(String(init.body)) : undefined });
    if (method === "DELETE" && path === "/backups/old%2Fbackup") {
      removed = true;
      return Promise.resolve(new Response(null, { status: 204 }));
    }
    if (method === "GET" && path === "/backups") return removed ? reload.promise : Promise.resolve(response(catalog));
    if (method === "POST" && path === "/backups") return Promise.resolve(response({ backup: { ...entry, id: "new", label: "replacement" } }));
    throw new Error(`Unexpected ${method} ${path}`);
  }));
  const port = new SsePort("http://kernel.example");
  const host = <Backup port={port} />;
  return { ...render(strict ? <StrictMode>{host}</StrictMode> : host), calls, reload, port };
}

function selected() {
  return screen.getAllByRole<HTMLInputElement>("checkbox").filter((field) => field.checked).map((field) => field.dataset.target);
}

async function clearSelection() {
  await screen.findByText("laptop");
  for (const field of screen.getAllByRole<HTMLInputElement>("checkbox")) if (field.checked) await userEvent.click(field);
  expect(selected()).toEqual([]);
}

async function remove(calls: { method: string }[], reload: ReturnType<typeof deferred<Response>>) {
  await userEvent.click(screen.getByRole("button", { name: "删除" }));
  await userEvent.click(screen.getByRole("button", { name: "确认删除" }));
  expect(calls.filter((call) => call.method === "DELETE")).toHaveLength(1);
  await act(async () => reload.resolve(response({ ...catalog, backups: [] })));
  expect(screen.queryByText("laptop")).toBeNull();
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  localStorage.setItem(STORAGE, "zh");
  boot();
});

it.each([false, true])("retains an explicitly empty selection after deleting an old backup (StrictMode=%s)", async (strict) => {
  const { calls, reload } = draw(strict);
  await clearSelection();
  for (const field of screen.getAllByLabelText<HTMLInputElement>(/加密口令|再输一次/)) await userEvent.type(field, "a long passphrase");
  await remove(calls, reload);
  expect(selected()).toEqual([]);
  expect(screen.getByRole<HTMLButtonElement>("button", { name: "备份到账号" }).disabled).toBe(true);
  expect(screen.getByText("至少选择一项备份内容")).toBeTruthy();
  expect(calls.filter((call) => call.method === "POST")).toHaveLength(0);
  await userEvent.click(screen.getByRole("checkbox", { name: /技能、插件与 MCP/ }));
  await userEvent.click(screen.getByRole("button", { name: "备份到账号" }));
  await screen.findByText("已备份。");
  const sent = calls.find((call) => call.method === "POST")!.body as BackupCreateRequest;
  expect(sent).toEqual({ label: "", categories: ["extensions"], passphrase: "a long passphrase" });
});

it.each([false, true])("retains nonempty explicit categories and secret consent across the same reload (StrictMode=%s)", async (strict) => {
  const { calls, reload } = draw(strict);
  await clearSelection();
  await userEvent.click(screen.getByRole("checkbox", { name: /长期记忆/ }));
  await userEvent.click(screen.getByRole("checkbox", { name: /API 密钥/ }));
  await remove(calls, reload);
  expect(selected()).toEqual(["memory", "secrets"]);
  expect(calls.filter((call) => call.method === "POST")).toHaveLength(0);
});

it.each([false, true])("initializes defaults again for a different kernel connection (StrictMode=%s)", async (strict) => {
  const { rerender } = draw(strict);
  await clearSelection();
  const host = <Backup port={new SsePort("http://other-kernel.example")} />;
  rerender(strict ? <StrictMode>{host}</StrictMode> : host);
  await screen.findByText("laptop");
  expect(selected()).toEqual(["settings", "extensions", "memory", "automation"]);
  expect(screen.getByRole<HTMLInputElement>("checkbox", { name: /API 密钥/ }).checked).toBe(false);
});
