// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Chrome } from "./Chrome";
import { HttpError } from "../port/port";
import type { AgentPort, Preset, SessionStatus } from "../port/port";

afterEach(cleanup);

function deferred<T>() {
  let settle!: (v: T) => void;
  let fail!: (e: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    settle = res;
    fail = rej;
  });
  // A promise nobody rejects still counts as unhandled if the component drops
  // it, so the test has to be the one holding it.
  promise.catch(() => {});
  return { promise, settle, fail };
}

function draw(preset: Preset, setPreset: (p: Preset) => Promise<void>) {
  const onChanged = vi.fn();
  const port = { setPreset, workspaces: async () => null } as unknown as AgentPort;
  const view = render(
    <Chrome
      port={port}
      status={{ preset } as SessionStatus}
      steer={0}
      run="idle"
      theme="light"
      onTheme={() => {}}
      focus={false}
      onFocus={() => {}}
      onSettings={() => {}}
      account={null}
      onChanged={onChanged}
    />,
  );
  const group = () => within(screen.getByRole("group", { name: "执行设定" }));
  const at = (name: string) => group().getByRole("button", { name });
  return { onChanged, view, at, refusal: () => document.querySelector(".chrome .badge[data-err]:not([hidden])") };
}

describe("changing the execution preset from the chrome", () => {
  it("asks the kernel once, and does not move the selection on its own", async () => {
    const call = deferred<void>();
    const setPreset = vi.fn(() => call.promise);
    const { at } = draw("balanced", setPreset);

    await userEvent.click(at("交付"));
    expect(setPreset).toHaveBeenCalledTimes(1);
    expect(setPreset).toHaveBeenCalledWith("delivery");
    // Still the kernel's answer, not the click's: nothing has come back yet.
    expect(at("均衡").getAttribute("aria-pressed")).toBe("true");
    expect(at("交付").getAttribute("aria-pressed")).toBe("false");
  });

  it("shows the call is out and refuses a second one until it settles", async () => {
    const call = deferred<void>();
    const setPreset = vi.fn(() => call.promise);
    const { at } = draw("balanced", setPreset);

    await userEvent.click(at("交付"));
    expect(at("交付").hasAttribute("data-asking")).toBe(true);
    // A pair of one-of-two buttons has no meaning for two answers in flight.
    expect((at("交付") as HTMLButtonElement).disabled).toBe(true);
    expect((at("均衡") as HTMLButtonElement).disabled).toBe(true);

    await userEvent.click(at("均衡"));
    await userEvent.click(at("交付"));
    expect(setPreset).toHaveBeenCalledTimes(1);
  });

  it("hands the selection back to the kernel's own answer when it lands", async () => {
    const call = deferred<void>();
    const { at, onChanged, view } = draw("balanced", () => call.promise);

    await userEvent.click(at("交付"));
    call.settle();
    await waitFor(() => expect(onChanged).toHaveBeenCalledTimes(1));
    // onChanged is what makes the shell re-read /status; the selection follows
    // that read, which is why it is the prop and not local state.
    view.rerender(
      <Chrome
        port={{ setPreset: async () => {}, workspaces: async () => null } as unknown as AgentPort}
        status={{ preset: "delivery" } as SessionStatus}
        steer={0}
        run="idle"
        theme="light"
        onTheme={() => {}}
        focus={false}
        onFocus={() => {}}
        onSettings={() => {}}
        account={null}
        onChanged={onChanged}
      />,
    );
    expect(at("交付").getAttribute("aria-pressed")).toBe("true");
    expect((at("交付") as HTMLButtonElement).disabled).toBe(false);
  });

  it("keeps the old selection and says so when the kernel refuses", async () => {
    const call = deferred<void>();
    const { at, onChanged, refusal } = draw("balanced", () => call.promise);

    await userEvent.click(at("交付"));
    call.fail(new HttpError(409, "a turn is running", { code: "busy.switch_model" }));

    await waitFor(() => expect(refusal()).toBeTruthy());
    expect(refusal()?.textContent).toContain("任务正在运行");
    expect(at("均衡").getAttribute("aria-pressed")).toBe("true");
    // Usable again, and nothing told the shell to go and re-read a change that
    // did not happen.
    expect((at("交付") as HTMLButtonElement).disabled).toBe(false);
    expect(onChanged).not.toHaveBeenCalled();
  });
});
