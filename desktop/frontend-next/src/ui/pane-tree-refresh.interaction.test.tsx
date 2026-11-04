// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import "./testkit";
import { Pane } from "./Pane";
import { ToastStack } from "./ToastStack";
import { MockPort } from "../port/mock";
import { boot, STORAGE, t } from "../i18n";
import { reason } from "../i18n/kernel";
import { HttpError } from "../port/port";
import type { AgentPort } from "../port/port";
import type { WireEvent } from "../port/wire";
import type { RuntimeView } from "../port/hub";

afterEach(() => {
  cleanup();
  localStorage.setItem(STORAGE, "zh");
  boot();
});

const rt = { id: "p1", root: "/w", name: "w" } as RuntimeView;

function open() {
  const port = new MockPort();
  vi.spyOn(port, "history").mockResolvedValue([{ role: "user", content: "Existing conversation" }]);
  let emit: (ev: WireEvent) => void = () => {};
  const subscribe = port.subscribe.bind(port);
  vi.spyOn(port, "subscribe").mockImplementation((onEvent, onGap, bootstrap) => {
    emit = onEvent;
    return subscribe(onEvent, onGap, bootstrap);
  });
  const changes = vi.spyOn(port, "changes");
  render(<>
    <ToastStack onFeedback={() => {}} />
    <Pane port={port as AgentPort} rt={rt} title="w" active visible sideHost={null} side={false}
      onFocus={() => {}} onReport={() => {}} onSessionChanged={() => {}} pulse={0} findPulse={0}
      onSettings={() => {}} needsProject={false} onOpenProject={() => {}} onKeepHere={() => {}}
      theme="dark" dockW={560} dockMax={880} onDockW={() => {}} />
  </>);
  const send = (ev: object) => act(() => emit(ev as WireEvent));
  return { port, changes, send };
}

describe("the explorer during a turn", () => {
  it("re-reads the working tree when a call that may write returns, not when a read does", () => {
    const pane = open();
    pane.send({ kind: "turn_started" });
    pane.changes.mockClear();

    pane.send({ kind: "tool_dispatch", tool: { id: "r1", name: "read_file", readOnly: true } });
    pane.send({ kind: "tool_result", tool: { id: "r1", name: "read_file", readOnly: true, output: "ok" } });
    expect(pane.changes).not.toHaveBeenCalled();

    pane.send({ kind: "tool_dispatch", tool: { id: "w1", name: "write_file", readOnly: false } });
    pane.send({ kind: "tool_result", tool: { id: "w1", name: "write_file", readOnly: false, output: "ok" } });
    expect(pane.changes).toHaveBeenCalledTimes(1);
  });

  // The composer's branch reading rides the same refresh path: a switch made
  // in an external terminal shows up when the turn boundary re-reads the tree,
  // not live.
  it("updates the composer's branch chip after a turn ends", async () => {
    const pane = open();
    const chip = await screen.findByRole("button", { name: t("composer.configure_workspace") });
    expect(chip.textContent).toContain("main");

    pane.send({ kind: "turn_started" });
    await waitFor(() => expect(pane.changes).toHaveBeenCalled());
    pane.port.branchState = { ...pane.port.branchState, branch: "studio" };
    pane.send({ kind: "turn_done" });
    await waitFor(() => expect(chip.textContent).toContain("studio"));
  });
});


describe("branch switch results in the pane", () => {
  for (const changesFail of [false, true]) {
    it(`uses the switch response while the change list ${changesFail ? "fails" : "is pending"}`, async () => {
      const pane = open();
      const chip = await screen.findByRole("button", { name: t("composer.configure_workspace") });
      const reading = vi.spyOn(pane.port, "workspaceGit").mockImplementation(() => new Promise(() => {}));
      pane.changes.mockClear();
      pane.changes.mockImplementation(() => changesFail
        ? Promise.reject(new Error("change list unavailable"))
        : new Promise(() => {}));
      const change = vi.spyOn(pane.port, "switchBranch");

      fireEvent.click(chip);
      fireEvent.click(await screen.findByRole("option", { name: "studio" }));

      await waitFor(() => expect(chip.textContent).toContain("studio"));
      expect(change).toHaveBeenCalledExactlyOnceWith("studio");
      expect(pane.changes).toHaveBeenCalledTimes(1);
      expect(reading).not.toHaveBeenCalled();
      await waitFor(() => expect(chip.hasAttribute("data-pending")).toBe(false));

      pane.send({ kind: "turn_started" });
      pane.send({ kind: "turn_done" });
      await waitFor(() => expect(reading).toHaveBeenCalled());
    });
  }
});

describe("branch switch in flight", () => {
  it("says which branch it is moving to until the switch answers, then names the branch", async () => {
    const pane = open();
    const chip = await screen.findByRole("button", { name: t("composer.configure_workspace") });
    let land: (git: Awaited<ReturnType<typeof pane.port.switchBranch>>) => void = () => {};
    const done = new Promise<Awaited<ReturnType<typeof pane.port.switchBranch>>>((resolve) => { land = resolve; });
    vi.spyOn(pane.port, "switchBranch").mockImplementation(() => done);

    fireEvent.click(chip);
    fireEvent.click(await screen.findByRole("option", { name: "studio" }));

    await waitFor(() => expect(chip.textContent).toContain("正在切换到 studio…"));
    expect(chip.title).toContain("main");
    expect(chip.hasAttribute("data-pending")).toBe(true);

    land({ ...pane.port.branchState, branch: "studio" });
    await waitFor(() => expect(chip.textContent).not.toContain("正在切换"));
    expect(chip.textContent).toContain("studio");
  });

  it("goes back to the current branch when the switch is refused", async () => {
    const pane = open();
    const chip = await screen.findByRole("button", { name: t("composer.configure_workspace") });
    vi.spyOn(pane.port, "switchBranch").mockRejectedValue(new HttpError(409, "busy", { code: "branch.turn_running", error: "busy" }));
    fireEvent.click(chip);
    fireEvent.click(await screen.findByRole("option", { name: "studio" }));
    await waitFor(() => expect(chip.textContent).not.toContain("正在切换"));
    expect(chip.textContent).toContain("main");
  });
});

describe("branch switch refusals in the pane", () => {
  for (const language of ["zh", "en"]) {
    for (const code of ["branch.workspace_busy", "branch.jobs_running", "branch.turn_running"]) {
      it(`shows ${code} in ${language} and lets the reader retry`, async () => {
        localStorage.setItem(STORAGE, language);
        boot();
        const pane = open();
        const error = new HttpError(409, "fixture fallback", { code, error: "fixture fallback" });
        const refusal = reason(error);
        expect(refusal).not.toBe("fixture fallback");
        const change = vi.spyOn(pane.port, "switchBranch").mockRejectedValue(error);
        const chip = await screen.findByRole("button", { name: t("composer.configure_workspace") });
        fireEvent.click(chip);
        fireEvent.click(await screen.findByRole("option", { name: "studio" }));
        expect(await screen.findByText(refusal)).toBeTruthy();
        expect(change).toHaveBeenCalledWith("studio");
        expect(screen.queryByText("fixture fallback")).toBeNull();
        await waitFor(() => expect(chip.hasAttribute("disabled")).toBe(false));
        expect(chip.textContent).toContain("main");
        fireEvent.click(await screen.findByRole("option", { name: "studio" }));
        await waitFor(() => expect(change).toHaveBeenCalledTimes(2));
      });
    }
  }
});
