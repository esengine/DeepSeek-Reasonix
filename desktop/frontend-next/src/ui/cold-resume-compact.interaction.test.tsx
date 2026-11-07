// @vitest-environment jsdom
import "./testkit";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MockPort } from "../port/mock";
import type { WireEvent } from "../port/wire";
import type { Item } from "../state/session";
import { liveColdResumeOffer } from "./CompactNow";
import { ToastStack } from "./ToastStack";
import { Pane } from "./Pane";

afterEach(() => { cleanup(); localStorage.clear(); vi.restoreAllMocks(); });

const payload = (compact: boolean, tokens = 240045) => JSON.stringify({ idle_seconds: 93600, tokens, window: 1000000, compact });
const cold = (compact: boolean): WireEvent => ({ kind: "notice", level: "info", code: "cold_resume", text: "resumed after 26h0m0s idle", detail: payload(compact) });

async function mount() {
  const port = new MockPort();
  const subscribers = new Set<(event: WireEvent) => void>();
  vi.spyOn(port, "subscribe").mockImplementation((onEvent) => {
    subscribers.add(onEvent);
    return () => subscribers.delete(onEvent);
  });
  const submit = vi.spyOn(port, "submit").mockResolvedValue();
  const view = render(<>
    <ToastStack onFeedback={() => {}} />
    <Pane port={port} rt={{ id: "cold", root: "/workspace", name: "Workspace", base: "/rt/cold" }}
      title="Workspace" active visible sideHost={null} side={false}
      onFocus={() => {}} onReport={() => {}} onSessionChanged={() => {}} pulse={0} findPulse={0}
      onSettings={() => {}} needsProject={false} onOpenProject={() => {}} onKeepHere={() => {}}
      theme="light" dockW={320} dockMax={640} onDockW={() => {}} />
  </>);
  await screen.findByRole("button", { name: "查看上下文与压缩" });
  const send = (event: WireEvent) => act(() => { subscribers.forEach((fn) => fn(event)); });
  const bar = () => view.container.querySelector<HTMLElement>(".notice-bar");
  return { submit, send, bar };
}

it("offers compaction on a cold resume the host measured as large, through the /compact path", async () => {
  const pane = await mount();
  pane.send(cold(true));
  await screen.findByText(/闲置 26h 后续聊/);
  const bar = within(pane.bar()!);
  expect(bar.getByText(/约 240k tokens/)).toBeTruthy();
  fireEvent.click(bar.getByRole("button", { name: "立即压缩" }));
  expect(pane.submit).not.toHaveBeenCalled();
  fireEvent.click(bar.getByRole("button", { name: "确认压缩" }));
  await waitFor(() => expect(pane.submit).toHaveBeenCalledExactlyOnceWith("/compact"));
  expect(bar.getByRole<HTMLButtonElement>("button", { name: "正在压缩…" }).disabled).toBe(true);
});

it("draws no button when the host did not offer a fold", async () => {
  const pane = await mount();
  pane.send(cold(false));
  await screen.findByText(/完整历史已保留/);
  expect(within(pane.bar()!).queryByRole("button")).toBeNull();
});

it("withdraws the offer once a fold has started, keeping the sentence", async () => {
  const pane = await mount();
  pane.send(cold(true));
  await within(pane.bar()!).findByRole("button", { name: "立即压缩" });
  pane.send({ kind: "compaction_started", compaction: { trigger: "auto" } });
  await waitFor(() => expect(within(pane.bar()!).queryByRole("button")).toBeNull());
  expect(screen.getByText(/闲置 26h 后续聊/)).toBeTruthy();
});

it("settles the notice's button on the same receipt as the context ring's", async () => {
  const pane = await mount();
  pane.send(cold(true));
  const bar = within(pane.bar()!);
  fireEvent.click(await bar.findByRole("button", { name: "立即压缩" }));
  fireEvent.click(bar.getByRole("button", { name: "确认压缩" }));
  await waitFor(() => expect(pane.submit).toHaveBeenCalledTimes(1));
  pane.send({ kind: "notice", code: "compact_declined", text: "nothing", detail: "input_unchanged" });
  await waitFor(() => expect(bar.getByRole<HTMLButtonElement>("button", { name: "立即压缩" }).disabled).toBe(false));
});

describe("the live cold-resume offer", () => {
  const notice = (id: string, compact: boolean): Extract<Item, { t: "notice" }> => ({ t: "notice", id, level: "info", text: "x", code: "cold_resume", detail: payload(compact) });
  const user: Item = { t: "user", id: "u", text: "next" };

  it("is the trailing offered notice", () => {
    expect(liveColdResumeOffer([user, notice("n", true)])).toBe("n");
  });

  it("lapses once a message was sent after it", () => {
    expect(liveColdResumeOffer([notice("n", true), user])).toBeUndefined();
  });

  it("lapses once a fold was drawn after it", () => {
    expect(liveColdResumeOffer([notice("n", true), { t: "compaction", id: "c", c: {}, done: true }])).toBeUndefined();
  });

  it("is absent when the latest cold resume made no offer", () => {
    expect(liveColdResumeOffer([notice("old", true), notice("new", false)])).toBeUndefined();
  });

  it("does not read the sentence", () => {
    expect(liveColdResumeOffer([{ ...notice("n", false), text: "/compact folds them first" }])).toBeUndefined();
  });
});
