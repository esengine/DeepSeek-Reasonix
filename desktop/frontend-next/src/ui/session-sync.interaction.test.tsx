// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import "./testkit";
import { App } from "./App";
import { MockHub } from "../port/mock_hub";
import type { TreeWorkspace } from "../port/hub";
import { boot, STORAGE } from "../i18n";

beforeEach(() => {
  localStorage.clear();
  sessionStorage.clear();
  localStorage.setItem(STORAGE, "zh");
  boot();
  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(null);
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  localStorage.clear();
  sessionStorage.clear();
});

it("converges two App trees and counts after another client's changes and page recovery", async () => {
  let book: TreeWorkspace[] = [{ root: "/project", name: "project", remembered: true, sessions: [
    { path: "/sessions/alpha.jsonl", name: "alpha", title: "Alpha" },
    { path: "/sessions/beta.jsonl", name: "beta", title: "Beta" },
  ] }];
  const listeners = new Set<() => void>();
  const clients = [0, 1].map(() => {
    const hub = new MockHub();
    hub.remoteHosts = async () => [];
    hub.tree = vi.fn(async () => structuredClone(book));
    hub.onTreeChanged = (fn) => {
      listeners.add(fn);
      return () => { listeners.delete(fn); };
    };
    const portFor = hub.portFor.bind(hub);
    hub.portFor = (runtime) => {
      const port = portFor(runtime);
      port.providerSetup = async () => null;
      port.welcomeSeen = async () => true;
      return port;
    };
    return hub;
  });
  const views = clients.map((hub) => render(<App hub={hub} />));
  const count = (i: number) => views[i].container.querySelector(".wsnode > .wsrow .rowcount")?.textContent;
  const ids = (i: number) => Array.from(views[i].container.querySelectorAll<HTMLElement>('[data-action-click="session.open"]'), (row) => row.dataset.target).sort();
  const notify = () => listeners.forEach((fn) => fn());
  await waitFor(() => clients.forEach((_, i) => expect(count(i)).toBe("2")));
  views.forEach((view) => {
    const row = view.container.querySelector('.wsrow')!;
    if (row.getAttribute("aria-expanded") === "false") fireEvent.click(row);
  });
  await waitFor(() => clients.forEach((_, i) => expect(ids(i)).toEqual(["/sessions/alpha.jsonl", "/sessions/beta.jsonl"])));

  await act(async () => {
    book[0].sessions[0].archived = true;
    book[0].sessions[1].pinned = true;
    notify();
  });
  await waitFor(() => clients.forEach((_, i) => {
    expect(count(i)).toBe("1");
    expect(views[i].container.querySelector('[data-action="session.pin"][aria-pressed="true"]')).toBeTruthy();
    expect(ids(i)).toEqual(["/sessions/beta.jsonl"]);
  }));
  await act(async () => {
    views[1].container.querySelector<HTMLButtonElement>('[data-value="archived"][data-action="session.filter"]')!.click();
  });
  await waitFor(() => {
    expect(ids(0)).toEqual(["/sessions/beta.jsonl"]);
    expect(ids(1)).toEqual(["/sessions/alpha.jsonl"]);
  });

  await act(async () => {
    book[0].sessions[0].archived = false;
    window.dispatchEvent(new Event("online"));
    window.dispatchEvent(new Event("pageshow"));
  });
  await waitFor(() => {
    expect(count(0)).toBe("2");
    expect(count(1)).toBeUndefined();
  });
  expect(ids(1)).toEqual([]);
  const reads = clients.map((hub) => vi.mocked(hub.tree).mock.calls.length);
  clients.forEach((hub) => vi.mocked(hub.tree).mockRejectedValueOnce(new Error("tree temporarily unavailable")));
  await act(async () => { notify(); });
  await waitFor(() => clients.forEach((hub, i) => expect(hub.tree).toHaveBeenCalledTimes(reads[i] + 1)));
  expect(count(0)).toBe("2");
  await act(async () => { book = []; notify(); });
  await waitFor(() => clients.forEach((_, i) => expect(count(i)).toBeUndefined()));
});


it("bounds tree reads when recovery events arrive after earlier reads finish", async () => {
  const hub = new MockHub();
  hub.remoteHosts = async () => [];
  hub.tree = vi.fn(async () => []);
  render(<App hub={hub} />);
  await waitFor(() => expect(hub.tree).toHaveBeenCalled());
  const before = vi.mocked(hub.tree).mock.calls.length;
  for (const event of ["focus", "online", "pageshow"]) {
    await act(async () => { window.dispatchEvent(new Event(event)); });
  }
  await waitFor(() => expect(hub.tree).toHaveBeenCalledTimes(before + 1));
});
