// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import "./testkit";
import { App } from "./App";
import { MockHub } from "../port/mock_hub";
import { boot, STORAGE } from "../i18n";
import { refresh } from "./viewport";

beforeEach(async () => {
  await import("./Settings");
  sessionStorage.clear();
  localStorage.clear();
  localStorage.setItem(STORAGE, "zh");
  boot();
  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(null);
  vi.spyOn(document.body, "clientWidth", "get").mockReturnValue(1600);
  vi.spyOn(document.body, "clientHeight", "get").mockReturnValue(900);
  refresh();
});
afterEach(() => {
  cleanup();
  sessionStorage.clear();
  localStorage.clear();
  vi.restoreAllMocks();
});

async function open() {
  const hub = new MockHub();
  const build = hub.portFor.bind(hub);
  const prepared = new WeakSet();
  hub.portFor = (rt) => {
    const port = build(rt);
    if (!prepared.has(port)) {
      prepared.add(port);
      port.providerSetup = async () => null;
      port.welcomeSeen = async () => true;
    }
    return port;
  };
  render(<App hub={hub} />);
  await screen.findByRole("combobox", { name: "任务输入" });
}

it("draws no handles on the reading column and ignores a width an older build remembered", async () => {
  localStorage.setItem("rx-flow-w", "900");
  await open();
  expect(document.querySelector(".flowgrip")).toBeNull();
  expect(document.querySelector<HTMLElement>(".app")!.style.getPropertyValue("--flow-w")).toBe("");
});
