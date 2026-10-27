// @vitest-environment jsdom
import { act, cleanup, fireEvent, renderHook } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { useHistoryAnchor, useOlderPageIntent } from "./useHistoryAnchor";
import type { HistoryPaging } from "./useHistoryPages";
import type { Item } from "../state/session";

afterEach(() => { cleanup(); document.body.innerHTML = ""; vi.unstubAllGlobals(); });

function fixture(contents = false) {
  vi.stubGlobal("CSS", { escape: (text: string) => text });
  const root = document.createElement("div");
  const row = document.createElement("div");
  row.dataset.item = "anchor";
  root.append(row); document.body.append(root);
  root.getBoundingClientRect = () => ({ top: 100, bottom: 500, height: 400 }) as DOMRect;
  row.getBoundingClientRect = () => ({ top: 120, bottom: 150, height: 30 }) as DOMRect;
  if (contents) {
    row.style.display = "contents";
    const visible = document.createElement("div");
    visible.getBoundingClientRect = row.getBoundingClientRect;
    row.append(visible);
    row.getBoundingClientRect = () => ({ top: 0, bottom: 0, height: 0 }) as DOMRect;
  }
  const item: Item = { t: "user", id: "anchor", text: "anchor" };
  const scroll = { current: root };
  const generation = { current: 0 };
  const release = vi.fn();
  const land = vi.fn();
  const paging: HistoryPaging = { snapshot: "one", revision: 1, hasMore: true, busy: false, failed: false, earlier: 0, more: vi.fn(async () => {}), reload: vi.fn(async () => {}) };
  const hook = renderHook(({ revision, snapshot = "one", hidden = false }) => useHistoryAnchor({ ...paging, revision, snapshot }, scroll, [[item]], hidden, generation, release, land),
    { initialProps: { revision: 1, snapshot: "one", hidden: false } });
  return { ...hook, generation, land, paging };
}

it("restores through the existing landing writer only after the new page is committed", () => {
  const f = fixture();
  act(() => f.result.current());
  expect(f.land).not.toHaveBeenCalled();
  f.rerender({ revision: 2, snapshot: "one", hidden: false });
  expect(f.land).toHaveBeenCalledWith(0, 0, '[data-item="anchor"]', 20, undefined, false);
});

it("anchors display-contents message wrappers using their visible child", () => {
  const f = fixture(true);
  act(() => f.result.current());
  f.rerender({ revision: 2, snapshot: "one", hidden: false });
  expect(f.land).toHaveBeenCalledWith(0, 0, '[data-item="anchor"]', 20, undefined, false);
});

it.each(["gesture", "binding", "hidden"])("does not overwrite a newer %s", (change) => {
  const f = fixture();
  act(() => f.result.current());
  if (change === "gesture") f.generation.current++;
  f.rerender({ revision: 2, snapshot: change === "binding" ? "two" : "one", hidden: change === "hidden" });
  expect(f.land).not.toHaveBeenCalled();
});

it("requests older pages only after reader intent near the top, and stops while hidden", () => {
  const root = document.createElement("div");
  document.body.append(root);
  const load = vi.fn();
  const generation = { current: 0 };
  const scroll = { current: root };
  const { rerender } = renderHook(({ hidden }) => useOlderPageIntent({ scroll, hidden, load, generation }), { initialProps: { hidden: false } });
  fireEvent.scroll(root);
  fireEvent.wheel(root, { deltaY: 20 });
  expect(load).not.toHaveBeenCalled();
  root.scrollTop = 300;
  fireEvent.wheel(root, { deltaY: -20 });
  expect(load).not.toHaveBeenCalled();
  root.scrollTop = 30;
  fireEvent.scroll(root);
  expect(load).toHaveBeenCalledTimes(1);
  rerender({ hidden: true });
  fireEvent.keyDown(root, { key: "Home" });
  expect(load).toHaveBeenCalledTimes(1);
  expect(generation.current).toBe(1);
});
