// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { SkillRow } from "./SkillRow";
import { HttpError } from "../port/port";
import type { AgentPort, ScopeLayer, SkillEntry } from "../port/port";

afterEach(cleanup);

function deferred<T>() {
  let settle!: (v: T) => void;
  let fail!: (e: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    settle = res;
    fail = rej;
  });
  promise.catch(() => {});
  return { promise, settle, fail };
}

const skill = (over: Partial<SkillEntry> = {}): SkillEntry => ({
  name: "explore",
  slashName: "explore",
  description: "看看周围有什么",
  scope: "project",
  enabled: true,
  ...over,
});

function draw(sk: SkillEntry, setSkillEnabled: AgentPort["setSkillEnabled"]) {
  const onDone = vi.fn();
  const onFailed = vi.fn();
  const port = { setSkillEnabled } as unknown as AgentPort;
  render(<SkillRow sk={sk} implicit port={port} onDone={onDone} root="/w/repo" onFailed={onFailed} />);
  const sw = () => screen.getByRole("switch") as HTMLButtonElement;
  return { onDone, onFailed, sw };
}

describe("switching one skill off", () => {
  // The switch is durable and scoped: flipping one this project inherits writes
  // a project row, because the answer was given for this folder.
  it("asks for this skill, off, at this project's layer", async () => {
    const call = deferred<void>();
    const setSkillEnabled = vi.fn(() => call.promise);
    const { sw } = draw(skill(), setSkillEnabled);

    await userEvent.click(sw());
    expect(setSkillEnabled).toHaveBeenCalledTimes(1);
    expect(setSkillEnabled).toHaveBeenCalledWith("explore", false, "project" satisfies ScopeLayer, "/w/repo");
  });

  it("refuses a second flip while the first is unanswered", async () => {
    const call = deferred<void>();
    const setSkillEnabled = vi.fn(() => call.promise);
    const { sw } = draw(skill(), setSkillEnabled);

    await userEvent.click(sw());
    expect(sw().disabled).toBe(true);
    // aria-checked still reads the answer the row was given, not the click.
    expect(sw().getAttribute("aria-checked")).toBe("true");
    await userEvent.click(sw());
    expect(setSkillEnabled).toHaveBeenCalledTimes(1);
  });

  it("asks for a re-read once the kernel has answered", async () => {
    const call = deferred<void>();
    const { sw, onDone, onFailed } = draw(skill(), () => call.promise);

    await userEvent.click(sw());
    expect(onDone).not.toHaveBeenCalled();
    call.settle();
    await waitFor(() => expect(onDone).toHaveBeenCalledTimes(1));
    expect(sw().disabled).toBe(false);
    expect(onFailed).toHaveBeenLastCalledWith("");
  });

  it("reports a refusal, leaves the row alone, and re-reads anyway", async () => {
    const call = deferred<void>();
    const { sw, onDone, onFailed } = draw(skill(), () => call.promise);

    await userEvent.click(sw());
    call.fail(new HttpError(400, "unknown skill: explore"));
    await waitFor(() => expect(onDone).toHaveBeenCalledTimes(1));

    // Only that a reason reached the panel. How it is worded is a separate
    // open item: this row hands on err.message rather than going through
    // i18n/kernel's reason(), which some twenty catch sites also do.
    const said = onFailed.mock.calls.at(-1)?.[0] as string;
    expect(said).toBeTruthy();
    // The row keeps the state the kernel last gave it; nothing was flipped on
    // the strength of a click that was turned down.
    expect(sw().getAttribute("aria-checked")).toBe("true");
    expect(sw().disabled).toBe(false);
  });
});
