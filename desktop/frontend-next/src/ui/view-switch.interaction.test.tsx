// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { App } from "./App";
import { ViewSwitch } from "./ViewSwitch";
import { MockHub } from "../port/mock_hub";

afterEach(cleanup);

function readyHub() {
  const hub = new MockHub();
  const build = hub.portFor.bind(hub);
  hub.portFor = (rt) => {
    const port = build(rt);
    port.providerSetup = async () => null;
    port.welcomeSeen = async () => true;
    return port;
  };
  return hub;
}

const switchButton = (to: string) => document.querySelector<HTMLButtonElement>(`.chrome [data-action="pane.view"][data-value="${to}"]`);

describe("the window bar's view switch", () => {
  it("replaces the strip above the pane: no view tabs and no plan peek remain", async () => {
    render(<App hub={readyHub()} />);
    await screen.findByRole("button", { name: "工作台" });
    expect(document.querySelector(".pane > .tabs")).toBeNull();
    expect(document.querySelector(".pane .plan-peek")).toBeNull();
    expect(document.querySelector('.pane [role="tab"][data-value]')).toBeNull();
  });

  it("opens the workbench from the bar and the same button closes it again", async () => {
    const user = userEvent.setup();
    render(<App hub={readyHub()} />);
    const button = await screen.findByRole("button", { name: "工作台" });
    expect(button.getAttribute("aria-pressed")).toBe("false");
    await user.click(button);
    await waitFor(() => expect(switchButton("browser")!.getAttribute("aria-pressed")).toBe("true"));
    await waitFor(() => expect(document.querySelector(".workbench-launcher, .workbench-body")).not.toBeNull());
    await user.click(switchButton("browser")!);
    await waitFor(() => expect(switchButton("browser")!.getAttribute("aria-pressed")).toBe("false"));
  });

  it("draws no run analysis button while the conversation is empty", async () => {
    render(<App hub={readyHub()} />);
    await screen.findByRole("button", { name: "工作台" });
    expect(switchButton("analysis")).toBeNull();
  });

  it("names its buttons by label and shortcut, not by icon alone", async () => {
    render(<App hub={readyHub()} />);
    const button = await screen.findByRole("button", { name: "工作台" });
    fireEvent.focus(button);
    const tip = document.querySelector(".icon-tooltip");
    expect(tip?.textContent).toContain("工作台");
    expect(tip?.querySelector("kbd")?.textContent).toMatch(/Ctrl 3|⌘3/);
    expect(button.getAttribute("aria-keyshortcuts")).toBeTruthy();
  });
});

describe("ViewSwitch on its own", () => {
  it("draws analysis once there are rows, shows the workbench count, and presses back to the conversation", async () => {
    const user = userEvent.setup();
    const onView = vi.fn();
    const { rerender } = render(<ViewSwitch view="flow" rows={0} surfaces={0} onView={onView} />);
    expect(screen.queryByRole("button", { name: "运行分析" })).toBeNull();
    rerender(<ViewSwitch view="flow" rows={3} surfaces={2} onView={onView} />);
    expect(screen.getByRole("button", { name: "工作台" }).textContent).toBe("2");
    // The analysis entry reads as words, not only an icon: a control that exists
    // solely as a picture is one people scroll past.
    expect(screen.getByRole("button", { name: "运行分析" }).textContent).toBe("运行分析");
    await user.click(screen.getByRole("button", { name: "运行分析" }));
    expect(onView).toHaveBeenLastCalledWith("analysis");
    rerender(<ViewSwitch view="analysis" rows={3} surfaces={2} onView={onView} />);
    expect(screen.getByRole("button", { name: "运行分析" }).getAttribute("aria-pressed")).toBe("true");
    await user.click(screen.getByRole("button", { name: "运行分析" }));
    expect(onView).toHaveBeenLastCalledWith("flow");
  });
});
