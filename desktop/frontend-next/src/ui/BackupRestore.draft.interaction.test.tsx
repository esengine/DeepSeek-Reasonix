// @vitest-environment jsdom
import { StrictMode } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { BackupRestore } from "./BackupRestore";
import { SsePort } from "../port/sse";
import { boot, STORAGE } from "../i18n";
import type { BackupEntry, BackupPlan } from "../port/port";

const entry: BackupEntry = {
  id: "saved/backup", label: "laptop", format: 1, appVersion: "2.29.0", platform: "darwin/arm64",
  categories: ["settings", "automation", "memory"], ciphertextBytes: 2048, createdAt: "2026-10-01T00:00:00Z",
};
const plan: BackupPlan = {
  planId: "restore-ticket", createdAt: entry.createdAt, platform: entry.platform, samePlatform: true,
  categories: entry.categories, items: [
    { id: "interface", category: "settings", kind: "interface", name: "Interface", status: "changed", recommended: true },
    { id: "hook:PreToolUse#1", category: "automation", kind: "hook", name: "PreToolUse#1", status: "new", consent: "executes", summary: "npx prettier --check .", recommended: false },
    { id: "memory:docs/REASONIX.md", category: "memory", kind: "memory", name: "REASONIX.md", status: "new", recommended: true },
  ],
};

function reply(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((yes) => { resolve = yes; });
  return { promise, resolve };
}

function draw(strict: boolean, held: "preview" | "apply" = "preview") {
  localStorage.setItem(STORAGE, "zh");
  boot();
  const pending = deferred<Response>();
  const previews: { passphrase: string }[] = [];
  const applies: { planId: string; items: string[]; consented: string[] }[] = [];
  vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const path = new URL(String(input)).pathname;
    if (init?.method === "POST" && path === "/backups/saved%2Fbackup/preview") {
      previews.push(JSON.parse(String(init.body)) as { passphrase: string });
      return held === "preview" ? pending.promise : Promise.resolve(reply(plan));
    }
    if (init?.method === "POST" && path === "/backups/apply") {
      applies.push(JSON.parse(String(init.body)) as typeof applies[number]);
      return pending.promise;
    }
    throw new Error(`Unexpected ${init?.method} ${path}`);
  }));
  const onClose = vi.fn();
  const host = <BackupRestore port={new SsePort("http://kernel.example")} backup={entry} onClose={onClose} />;
  return { ...render(strict ? <StrictMode>{host}</StrictMode> : host), pending, previews, applies, onClose };
}

function pick(id: string) {
  return document.querySelector<HTMLInputElement>(`[data-action="backup.pick"][data-target="${id}"]`)!;
}

function choice() {
  return {
    picked: Array.from(document.querySelectorAll<HTMLInputElement>('[data-action="backup.pick"]')).filter((input) => input.checked).map((input) => input.dataset.target),
    consented: Array.from(document.querySelectorAll<HTMLInputElement>('[data-action="backup.allow"]')).filter((input) => input.checked).map((input) => input.dataset.target),
  };
}

async function preview() {
  await userEvent.type(screen.getByLabelText("加密口令"), "a long passphrase");
  await userEvent.click(screen.getByRole("button", { name: "预览差异" }));
}

async function prepareApply() {
  await preview();
  await screen.findByText("PreToolUse#1");
  await userEvent.click(pick("hook:PreToolUse#1"));
  expect(screen.getByRole<HTMLButtonElement>("button", { name: /恢复所选/ }).disabled).toBe(true);
  await userEvent.click(screen.getByRole("checkbox", { name: /允许它在本机运行/ }));
  return choice();
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  localStorage.setItem(STORAGE, "zh");
  boot();
});

it.each([false, true])("holds the passphrase until preview succeeds (StrictMode=%s)", async (strict) => {
  const { pending, previews } = draw(strict);
  await preview();
  expect(previews).toEqual([{ passphrase: "a long passphrase" }]);
  await userEvent.type(screen.getByLabelText("加密口令"), " not captured");
  const during = screen.getByLabelText<HTMLInputElement>("加密口令").value;
  expect(screen.getByRole<HTMLButtonElement>("button", { name: "取消" }).disabled).toBe(false);
  await act(async () => pending.resolve(reply(plan)));
  await screen.findByText("PreToolUse#1");
  expect(screen.queryByLabelText("加密口令")).toBeNull();
  expect(during).toBe("a long passphrase");
  await userEvent.click(pick("interface"));
  expect(pick("interface").checked).toBe(false);
  expect(previews).toHaveLength(1);
});

it.each([false, true])("preserves and unlocks the passphrase after preview refusal (StrictMode=%s)", async (strict) => {
  const { pending, previews } = draw(strict);
  await preview();
  expect(previews).toEqual([{ passphrase: "a long passphrase" }]);
  await userEvent.type(screen.getByLabelText("加密口令"), " not captured");
  await act(async () => pending.resolve(reply({ code: "backup.cloud_unavailable", error: "controlled cloud failure", params: { detail: "controlled cloud failure" } }, 502)));
  await screen.findByText("暂时连不上备份服务，稍后再试：controlled cloud failure");
  expect(screen.getByLabelText<HTMLInputElement>("加密口令").value).toBe("a long passphrase");
  await userEvent.clear(screen.getByLabelText("加密口令"));
  await userEvent.type(screen.getByLabelText("加密口令"), "retry passphrase");
  expect(screen.getByLabelText<HTMLInputElement>("加密口令").value).toBe("retry passphrase");
  expect(screen.getByRole<HTMLButtonElement>("button", { name: "预览差异" }).disabled).toBe(false);
  expect(previews).toHaveLength(1);
});

it.each([false, true])("holds selected items and consents until apply succeeds (StrictMode=%s)", async (strict) => {
  const { pending, applies } = draw(strict, "apply");
  const initial = await prepareApply();
  await userEvent.click(screen.getByRole("button", { name: /恢复所选/ }));
  expect(applies).toEqual([{ planId: plan.planId, items: initial.picked, consented: initial.consented }]);
  await userEvent.click(screen.getByRole("checkbox", { name: /允许它在本机运行/ }));
  await userEvent.click(pick("interface"));
  await userEvent.click(pick("memory:docs/REASONIX.md"));
  const during = choice();
  expect(screen.getByRole<HTMLButtonElement>("button", { name: "取消" }).disabled).toBe(false);
  await act(async () => pending.resolve(reply({ applied: initial.picked })));
  await screen.findByText("已恢复 3 项。");
  expect(during).toEqual(initial);
  expect(applies).toHaveLength(1);
});

it.each([false, true])("preserves and unlocks item choices after apply refusal (StrictMode=%s)", async (strict) => {
  const { pending, applies } = draw(strict, "apply");
  const initial = await prepareApply();
  await userEvent.click(screen.getByRole("button", { name: /恢复所选/ }));
  expect(applies).toEqual([{ planId: plan.planId, items: initial.picked, consented: initial.consented }]);
  await userEvent.click(screen.getByRole("checkbox", { name: /允许它在本机运行/ }));
  await userEvent.click(pick("interface"));
  await act(async () => pending.resolve(reply({ code: "backup.plan_expired", error: "restore preview expired" }, 410)));
  await screen.findByText("预览已失效，请重新打开这份备份");
  expect(choice()).toEqual(initial);
  await userEvent.click(screen.getByRole("checkbox", { name: /允许它在本机运行/ }));
  expect(choice().consented).toEqual([]);
  expect(screen.getByRole<HTMLButtonElement>("button", { name: /恢复所选/ }).disabled).toBe(true);
  await userEvent.click(pick("hook:PreToolUse#1"));
  expect(pick("hook:PreToolUse#1").checked).toBe(false);
  expect(screen.getByRole<HTMLButtonElement>("button", { name: /恢复所选/ }).disabled).toBe(false);
  expect(applies).toHaveLength(1);
});

it.each([false, true])("keeps normal selection and consent editing available (StrictMode=%s)", async (strict) => {
  const { applies } = draw(strict, "apply");
  const initial = await prepareApply();
  expect(initial.consented).toEqual(["hook:PreToolUse#1"]);
  await userEvent.click(pick("interface"));
  expect(pick("interface").checked).toBe(false);
  await userEvent.click(screen.getByRole("checkbox", { name: /允许它在本机运行/ }));
  expect(choice().consented).toEqual([]);
  expect(screen.getByRole<HTMLButtonElement>("button", { name: /恢复所选/ }).disabled).toBe(true);
  expect(applies).toHaveLength(0);
});
