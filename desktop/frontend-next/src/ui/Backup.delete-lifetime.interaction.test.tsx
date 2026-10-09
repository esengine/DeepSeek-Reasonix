// @vitest-environment jsdom
import { StrictMode } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Backup } from "./Backup";
import { SsePort } from "../port/sse";
import { boot, STORAGE } from "../i18n";
import type { BackupCatalog, BackupEntry } from "../port/port";

const backups: BackupEntry[] = ["one/backup", "two/backup"].map((id, index) => ({
  id, label: index === 0 ? "first laptop" : "second laptop", format: 1, appVersion: "2.29.0",
  platform: "darwin/arm64", categories: ["settings"], ciphertextBytes: 2048, createdAt: "2026-10-01T00:00:00Z",
}));

function reply(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });
}

function draw(strict: boolean) {
  localStorage.setItem(STORAGE, "zh");
  boot();
  const removed = new Set<string>();
  const sent: { id: string; resolve: (response: Response) => void }[] = [];
  vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const path = new URL(String(input)).pathname;
    const method = init?.method ?? "GET";
    if (method === "GET" && path === "/backups") {
      const catalog: BackupCatalog = {
        categories: [{ id: "settings", defaultOn: true, consent: false }],
        backups: backups.filter((backup) => !removed.has(backup.id)),
        limits: { maxCount: 10, maxBytes: 1048576 }, minPassphrase: 10,
      };
      return Promise.resolve(reply(catalog));
    }
    if (method === "DELETE" && backups.some((backup) => path === "/backups/" + encodeURIComponent(backup.id))) {
      const id = decodeURIComponent(path.slice("/backups/".length));
      return new Promise<Response>((resolve) => sent.push({ id, resolve }));
    }
    throw new Error(`Unexpected ${method} ${path}`);
  }));
  const host = <Backup port={new SsePort("http://kernel.example")} />;
  return {
    ...render(strict ? <StrictMode>{host}</StrictMode> : host), sent,
    async finish(index: number, status = 204) {
      const request = sent[index]!;
      await act(async () => {
        if (status === 204) {
          removed.add(request.id);
          request.resolve(new Response(null, { status }));
        } else {
          request.resolve(reply({ code: "backup.cloud_unavailable", error: "controlled cloud failure", params: { detail: "controlled cloud failure" } }, status));
        }
      });
    },
  };
}

function drop(id = backups[0]!.id) {
  return document.querySelector<HTMLButtonElement>(`[data-action="backup.delete"][data-target="${id}"]`)!;
}

async function begin(id = backups[0]!.id) {
  await screen.findByText("first laptop");
  await userEvent.click(drop(id));
  expect(drop(id).textContent).toBe("确认删除");
  await userEvent.click(drop(id));
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  localStorage.setItem(STORAGE, "zh");
  boot();
});

it.each([false, true])("sends one deletion while its successful receipt is pending (StrictMode=%s)", async (strict) => {
  const { sent, finish } = draw(strict);
  await begin();
  expect(sent.map((request) => request.id)).toEqual([backups[0]!.id]);
  await userEvent.click(drop());
  await userEvent.click(drop());
  const during = sent.map((request) => request.id);
  await finish(0);
  await waitFor(() => expect(screen.queryByText("first laptop")).toBeNull());
  expect(screen.getByText("second laptop")).toBeTruthy();
  expect(during).toEqual([backups[0]!.id]);
});

it.each([false, true])("holds a pending deletion through mouse leave and allows a fresh retry after refusal (StrictMode=%s)", async (strict) => {
  const { sent, finish } = draw(strict);
  await begin();
  await userEvent.unhover(drop());
  await userEvent.click(drop());
  await userEvent.click(drop());
  const during = sent.map((request) => request.id);
  await finish(0, 502);
  await screen.findByText("暂时连不上备份服务，稍后再试：controlled cloud failure");
  expect(during).toEqual([backups[0]!.id]);
  expect(drop().textContent).toBe("删除");
  await begin();
  expect(sent.map((request) => request.id)).toEqual([backups[0]!.id, backups[0]!.id]);
  await finish(1);
  await waitFor(() => expect(screen.queryByText("first laptop")).toBeNull());
});

it.each([false, true])("keeps another backup confirmation when an earlier deletion is refused (StrictMode=%s)", async (strict) => {
  const { sent, finish } = draw(strict);
  await begin();
  await userEvent.click(drop(backups[1]!.id));
  expect(drop(backups[1]!.id).textContent).toBe("确认删除");
  await finish(0, 502);
  await screen.findByText("暂时连不上备份服务，稍后再试：controlled cloud failure");
  expect(drop(backups[1]!.id).textContent).toBe("确认删除");
  await userEvent.click(drop(backups[1]!.id));
  expect(sent.map((request) => request.id)).toEqual([backups[0]!.id, backups[1]!.id]);
  await finish(1);
  await waitFor(() => expect(screen.queryByText("second laptop")).toBeNull());
  expect(screen.getByText("first laptop")).toBeTruthy();
});

it.each([false, true])("keeps distinct pending deletions independent when receipts arrive in reverse order (StrictMode=%s)", async (strict) => {
  const { sent, finish } = draw(strict);
  await begin();
  await userEvent.click(drop(backups[1]!.id));
  await userEvent.click(drop(backups[1]!.id));
  expect(sent.map((request) => request.id)).toEqual([backups[0]!.id, backups[1]!.id]);
  await finish(1);
  await waitFor(() => expect(screen.queryByText("second laptop")).toBeNull());
  await userEvent.click(drop());
  await userEvent.click(drop());
  const during = sent.map((request) => request.id);
  await finish(0, 502);
  await screen.findByText("暂时连不上备份服务，稍后再试：controlled cloud failure");
  expect(during).toEqual([backups[0]!.id, backups[1]!.id]);
  expect(drop().textContent).toBe("删除");
});

it.each([false, true])("retains the existing one-confirmation and mouse-leave cancellation flow (StrictMode=%s)", async (strict) => {
  const { sent } = draw(strict);
  await screen.findByText("first laptop");
  await userEvent.click(drop());
  await userEvent.click(drop(backups[1]!.id));
  expect(drop().textContent).toBe("删除");
  expect(drop(backups[1]!.id).textContent).toBe("确认删除");
  await userEvent.unhover(drop(backups[1]!.id));
  expect(drop(backups[1]!.id).textContent).toBe("删除");
  expect(sent).toHaveLength(0);
});

it.each([false, true])("keeps backup drafts and restore controls available during deletion (StrictMode=%s)", async (strict) => {
  const { sent, finish } = draw(strict);
  await begin();
  const label = screen.getByRole<HTMLInputElement>("textbox", { name: "备注（可选）" });
  await userEvent.type(label, "independent draft");
  await userEvent.click(screen.getByRole("checkbox", { name: /模型与界面设置/ }));
  expect(label.value).toBe("independent draft");
  expect(screen.getByRole<HTMLInputElement>("checkbox", { name: /模型与界面设置/ }).checked).toBe(false);
  expect(Array.from(document.querySelectorAll<HTMLButtonElement>('[data-action="backup.restore"]')).every((button) => !button.disabled)).toBe(true);
  expect(sent).toHaveLength(1);
  await finish(0, 502);
});

it.each([false, true])("does not resend deletion through repeated keyboard activation (StrictMode=%s)", async (strict) => {
  const { sent, finish } = draw(strict);
  await screen.findByText("first laptop");
  drop().focus();
  await userEvent.keyboard("{Enter}");
  expect(drop().textContent).toBe("确认删除");
  await userEvent.keyboard("{Enter}");
  expect(sent.map((request) => request.id)).toEqual([backups[0]!.id]);
  await userEvent.keyboard("{Enter}{Enter}");
  const during = sent.map((request) => request.id);
  await finish(0, 502);
  await screen.findByText("暂时连不上备份服务，稍后再试：controlled cloud failure");
  expect(during).toEqual([backups[0]!.id]);
});
