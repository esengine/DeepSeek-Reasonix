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

function reply(body: unknown, status = 200) {
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
  const pending = deferred<Response>();
  const sent: BackupCreateRequest[] = [];
  vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const path = new URL(String(input)).pathname;
    const method = init?.method ?? "GET";
    if (path === "/backups" && method === "GET") return Promise.resolve(reply(catalog));
    if (path === "/backups" && method === "POST") {
      sent.push(JSON.parse(String(init!.body)) as BackupCreateRequest);
      return pending.promise;
    }
    throw new Error(`Unexpected ${method} ${path}`);
  }));
  const port = new SsePort("http://kernel.example");
  const host = <Backup port={port} />;
  return { ...render(strict ? <StrictMode>{host}</StrictMode> : host), pending, sent };
}

function fields() {
  return {
    label: screen.getByRole<HTMLInputElement>("textbox", { name: "备注（可选）" }),
    pass: screen.getByLabelText<HTMLInputElement>(/加密口令（至少/),
    again: screen.getByLabelText<HTMLInputElement>("再输一次"),
  };
}

function snapshot() {
  const { label, pass, again } = fields();
  return {
    label: label.value, pass: pass.value, again: again.value,
    categories: screen.getAllByRole<HTMLInputElement>("checkbox").filter((c) => c.checked).map((c) => c.dataset.target),
  };
}

async function begin(sent: BackupCreateRequest[]) {
  await screen.findByText("laptop");
  const { label, pass, again } = fields();
  await userEvent.type(label, "captured draft");
  await userEvent.type(pass, "a long passphrase");
  await userEvent.type(again, "a long passphrase");
  const initial = snapshot();
  await userEvent.click(screen.getByRole("button", { name: "备份到账号" }));
  expect(sent).toEqual([{
    label: "captured draft", categories: ["settings", "extensions", "memory", "automation"], passphrase: "a long passphrase",
  }]);
  return initial;
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  localStorage.setItem(STORAGE, "zh");
  boot();
});

it.each([false, true])("holds the captured backup draft until successful creation (StrictMode=%s)", async (strict) => {
  const { pending, sent } = draw(strict);
  const initial = await begin(sent);
  const { label, pass, again } = fields();
  await userEvent.type(label, " not captured");
  await userEvent.type(pass, " not captured");
  await userEvent.type(again, " not captured");
  await userEvent.click(screen.getByRole("checkbox", { name: /API 密钥/ }));
  await userEvent.click(screen.getByRole("checkbox", { name: /长期记忆/ }));
  const during = snapshot();
  expect(screen.getByRole<HTMLButtonElement>("button", { name: "恢复…" }).disabled).toBe(false);
  expect(screen.getByRole<HTMLButtonElement>("button", { name: "删除" }).disabled).toBe(false);
  await act(async () => pending.resolve(reply({ backup: { ...entry, id: "created" } })));
  await screen.findByText("已备份。");
  expect(fields().label.value).toBe("");
  expect(fields().pass.value).toBe("");
  expect(fields().again.value).toBe("");
  expect(sent).toHaveLength(1);
  expect(during).toEqual(initial);
  await userEvent.type(fields().label, "next draft");
  expect(fields().label.value).toBe("next draft");
});

it.each([false, true])("preserves and unlocks the captured draft after refusal (StrictMode=%s)", async (strict) => {
  const { pending, sent } = draw(strict);
  const initial = await begin(sent);
  await userEvent.type(fields().label, " not captured");
  const during = snapshot();
  await act(async () => pending.resolve(reply({ code: "backup.cloud_unavailable", error: "controlled cloud failure", params: { detail: "controlled cloud failure" } }, 502)));
  await screen.findByText("暂时连不上备份服务，稍后再试：controlled cloud failure");
  expect(snapshot()).toEqual(initial);
  expect(during).toEqual(initial);
  await userEvent.clear(fields().label);
  await userEvent.type(fields().label, "retry draft");
  expect(fields().label.value).toBe("retry draft");
  expect(screen.getByRole<HTMLButtonElement>("button", { name: "备份到账号" }).disabled).toBe(false);
  expect(sent).toHaveLength(1);
});

it.each([false, true])("keeps normal backup draft editing available (StrictMode=%s)", async (strict) => {
  const { sent } = draw(strict);
  await screen.findByText("laptop");
  await userEvent.type(fields().label, "editable");
  await userEvent.type(fields().pass, "a long passphrase");
  await userEvent.type(fields().again, "a long passphrase");
  await userEvent.click(screen.getByRole("checkbox", { name: /API 密钥/ }));
  expect(fields().label.value).toBe("editable");
  expect(screen.getByRole<HTMLInputElement>("checkbox", { name: /API 密钥/ }).checked).toBe(true);
  expect(screen.getByRole<HTMLButtonElement>("button", { name: "备份到账号" }).disabled).toBe(false);
  expect(sent).toHaveLength(0);
});
