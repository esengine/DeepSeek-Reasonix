// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, waitFor } from "@testing-library/react";
import "./testkit";
import { Notifications } from "./Notifications";
import type { AgentPort, NotifyPrefs } from "../port/port";

afterEach(cleanup);

const OWED: NotifyPrefs = { enabled: false, turnDone: true, approval: true, ask: true };

function fakePort(answer: NotifyPrefs | null) {
  let held = answer;
  const set = vi.fn(async (next: NotifyPrefs) => {
    held = next;
    return held;
  });
  return { port: { notifyPrefs: async () => held, setNotifyPrefs: set } as unknown as AgentPort, set };
}

const box = async (answer: NotifyPrefs | null) => {
  const { port, set } = fakePort(answer);
  const view = render(<Notifications port={port} />);
  await waitFor(() => expect(view.container.querySelector("section, .none")).toBeDefined());
  return { view, set };
};

describe("the notifications block", () => {
  it("draws nothing where the kernel has no window to notify from", async () => {
    const { view } = await box(null);
    await waitFor(() => expect(view.container.querySelector("#set-notify")).toBeNull());
  });

  it("keeps the three occasions as branches of the one switch", async () => {
    const { view } = await box(OWED);
    await waitFor(() => expect(view.container.querySelector("#set-notify")).not.toBeNull());
    const branches = [...view.container.querySelectorAll(".lrow.subrow")];
    expect(branches.length).toBe(3);
    // Off, so the branches are drawn as not currently doing anything rather
    // than removed — a control that vanishes is one nobody knows exists.
    expect(branches.every((b) => b.hasAttribute("data-off"))).toBe(true);
  });

  it("writes what the person asked for and keeps what the host answered", async () => {
    const { view, set } = await box(OWED);
    await waitFor(() => expect(view.container.querySelector('[data-action="notify.enabled"]')).not.toBeNull());
    (view.container.querySelector('[data-action="notify.enabled"]') as HTMLElement).click();
    await waitFor(() => expect(set).toHaveBeenCalledWith({ ...OWED, enabled: true }));
    await waitFor(() => expect(view.container.querySelector(".lrow.subrow")?.hasAttribute("data-off")).toBe(false));
  });
});
