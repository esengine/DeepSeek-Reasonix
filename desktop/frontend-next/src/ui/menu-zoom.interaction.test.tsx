// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import "./testkit";
import { Workspaces } from "./Workspaces";
import { MockHub } from "../port/mock_hub";
import type { HubPort, TreeWorkspace } from "../port/hub";

const ZOOM = 1.5;
const BOX = { width: 228 * ZOOM, height: 200 * ZOOM };

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  document.documentElement.style.removeProperty("--zoom");
});

function open() {
  document.documentElement.style.setProperty("--zoom", String(ZOOM));
  const real = Element.prototype.getBoundingClientRect;
  vi.spyOn(Element.prototype, "getBoundingClientRect").mockImplementation(function (this: Element) {
    if (this.classList.contains("session-pop")) return { ...BOX, left: 0, top: 0, right: BOX.width, bottom: BOX.height, x: 0, y: 0, toJSON: () => ({}) };
    return real.call(this);
  });
  const tree = [
    { root: "/w", name: "w", sessions: [{ path: "/w/s.jsonl", name: "s", title: "the session" }] } as TreeWorkspace,
  ];
  render(
    <Workspaces
      hub={new MockHub() as unknown as HubPort}
      tree={tree}
      treeRead
      runtimes={[]}
      active=""
      folded={new Set()}
      onFold={() => {}}
      reload={async () => {}}
      onOpen={async () => {}}
      onFocus={() => {}}
      onClose={async () => {}}
      liveIds={() => []}
      runs={{}}
      onRename={() => {}}
      onError={() => {}}
      adder={{ add: () => {}, close: () => {}, at: null } as never}
    />,
  );
}

describe("context menus at a zoomed interface", () => {
  it("writes the session menu at the click divided by the zoom", () => {
    open();
    fireEvent.contextMenu(screen.getByText("the session"), { clientX: 300, clientY: 240 });
    const menu = screen.getByRole("menu", { name: /./ });
    expect(parseFloat(menu.style.left) * ZOOM).toBeCloseTo(300);
    expect(parseFloat(menu.style.top) * ZOOM).toBeCloseTo(240);
  });

  it("flips the session menu back inside the viewport near the bottom edge", () => {
    open();
    fireEvent.contextMenu(screen.getByText("the session"), { clientX: 300, clientY: innerHeight - 20 });
    const menu = screen.getByRole("menu", { name: /./ });
    expect(parseFloat(menu.style.top) * ZOOM + BOX.height).toBeLessThanOrEqual(innerHeight);
  });
});
