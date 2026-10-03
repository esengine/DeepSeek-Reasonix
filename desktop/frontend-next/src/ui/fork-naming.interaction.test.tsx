// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { App } from "./App";
import { MockHub } from "../port/mock_hub";
import { boot, STORAGE } from "../i18n";
import type { Checkpoint } from "../port/port";

const forkErrors = vi.hoisted(() => [] as unknown[]);

vi.mock("./Pane", () => ({
  Pane: ({ active, onFork, alert }: { active: boolean; onFork?: (checkpoint: Checkpoint) => Promise<void>; alert?: ReactNode }) =>
    active && onFork ? <>{alert}<button onClick={() => void onFork({ turn: 0, msgIndex: 1, stamp: "stamp", prompt: "question", files: 1 }).catch((e) => forkErrors.push(e))}>create fork</button></> : null,
}));

afterEach(() => {
  cleanup();
  forkErrors.length = 0;
  sessionStorage.clear();
  vi.restoreAllMocks();
  localStorage.setItem(STORAGE, "zh");
  boot();
});

it.each([
  { pref: "zh", system: "en-US" },
  { pref: "en", system: "zh-CN" },
  { pref: "auto", system: "zh-Hant" },
  { pref: "auto", system: "en-US" },
])("keeps the normal fork title using $pref with system $system", async ({ pref, system }) => {
  vi.spyOn(navigator, "languages", "get").mockReturnValue([system]);
  localStorage.setItem(STORAGE, pref);
  boot();
  const kernel = new MockHub();
  const source = (await kernel.runtimes())[0];
  const portFor = kernel.portFor.bind(kernel);
  kernel.portFor = (rt) => {
    const port = portFor(rt);
    port.providerSetup = async () => null;
    const appearance = port.appearance.bind(port);
    port.appearance = async () => ({ ...await appearance(), language: pref });
    return port;
  };
  const rename = vi.spyOn(kernel, "renameSession").mockRejectedValue(new Error("fork must not rename"));
  vi.spyOn(kernel, "fork").mockImplementation(async () => kernel.open({ root: source.root, sessionPath: "/sessions/fork.jsonl" }));
  vi.spyOn(kernel, "tree").mockImplementation(async () => [{
    root: source.root, name: source.name, remembered: true,
    sessions: (await kernel.runtimes()).map((rt) => ({
      path: rt.sessionPath!, name: rt.id, title: rt.id === source.id ? "项目分析" : "正常自动标题",
      runtimeId: rt.id, turns: 2,
    })),
  }]);
  render(<App hub={kernel} />);
  await waitFor(() => expect(document.querySelector(".crumb b")?.textContent).toBe("项目分析"));
  await userEvent.click(await screen.findByRole("button", { name: "create fork" }));
  await waitFor(() => expect(document.querySelector(".crumb b")?.textContent).toBe("正常自动标题"));
  expect(rename).not.toHaveBeenCalled();
});

async function failureFixture() {
  localStorage.setItem(STORAGE, "zh");
  boot();
  const kernel = new MockHub();
  const source = (await kernel.runtimes())[0];
  const portFor = kernel.portFor.bind(kernel);
  kernel.portFor = (rt) => {
    const port = portFor(rt);
    port.providerSetup = async () => null;
    const appearance = port.appearance.bind(port);
    port.appearance = async () => ({ ...await appearance(), language: "zh" });
    return port;
  };
  vi.spyOn(kernel, "tree").mockImplementation(async () => [{
    root: source.root, name: source.name, remembered: true,
    sessions: (await kernel.runtimes()).map((rt) => ({
      path: rt.sessionPath!, name: rt.id,
      title: rt.id === source.id ? "项目分析" : "默认分支标题", runtimeId: rt.id, turns: 2,
    })),
  }]);
  const fork = vi.spyOn(kernel, "fork").mockImplementation(async () => kernel.open({ root: source.root, sessionPath: "/sessions/fork.jsonl" }));
  const rename = vi.spyOn(kernel, "renameSession");
  render(<App hub={kernel} />);
  await waitFor(() => expect(document.querySelector(".crumb b")?.textContent).toBe("项目分析"));
  return { kernel, fork, rename };
}

it("does not open a pane when creating the fork fails", async () => {
  const { kernel, fork, rename } = await failureFixture();
  const error = new Error("fork denied");
  fork.mockRejectedValue(error);
  await userEvent.click(screen.getByRole("button", { name: "create fork" }));
  await waitFor(() => expect(forkErrors).toEqual([error]));
  expect(rename).not.toHaveBeenCalled();
  expect(await kernel.runtimes()).toHaveLength(1);
  expect(document.querySelector(".crumb b")?.textContent).toBe("项目分析");
});