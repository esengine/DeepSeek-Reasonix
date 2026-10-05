// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Policy } from "./Policy";
import { HttpError } from "../port/port";
import type { AgentPort, SessionStatus } from "../port/port";

afterEach(cleanup);

const STATUS = { preset: "balanced", effort: "medium", toolApprovalMode: "ask" } as SessionStatus;

function deferred<T>() {
  let settle!: (value: T) => void;
  let fail!: (error: unknown) => void;
  const promise = new Promise<T>((resolve, reject) => { settle = resolve; fail = reject; });
  promise.catch(() => {});
  return { promise, settle, fail };
}

function draw(calls: Partial<AgentPort> = {}) {
  const onChanged = vi.fn();
  const port = {
    setApprovalMode: vi.fn(async () => {}),
    ...calls,
  } as unknown as AgentPort;
  const view = render(<Policy port={port} status={STATUS} onChanged={onChanged} />);
  const open = () => userEvent.click(screen.getByRole("button", { name: "执行权限：询问" }));
  const option = (name: string) => within(screen.getByRole("group", { name: "执行权限" })).getByRole("button", { name: new RegExp(`^${name}`) });
  return { ...view, port, onChanged, open, option };
}

describe("execution permission changes", () => {
  it("changes only the approval mode", async () => {
    const { open, option, port } = draw();
    await open();
    await userEvent.click(option("自动批准"));
    expect(port.setApprovalMode).toHaveBeenCalledWith("auto");
  });

  it("does not submit a second permission while the first is pending", async () => {
    const call = deferred<void>();
    const setApprovalMode = vi.fn(() => call.promise);
    const { open, option } = draw({ setApprovalMode });
    await open();
    await userEvent.click(option("自动批准"));
    expect((option("询问") as HTMLButtonElement).disabled).toBe(true);
    await userEvent.click(option("询问"));
    expect(setApprovalMode).toHaveBeenCalledTimes(1);
    call.settle();
  });

  it("keeps the committed value and explains a refusal", async () => {
    const call = deferred<void>();
    const { open, option, container, onChanged } = draw({ setApprovalMode: () => call.promise });
    await open();
    await userEvent.click(option("全部放行"));
    call.fail(new HttpError(409, "a turn is running", { code: "busy.switch_model" }));
    await waitFor(() => expect(container.querySelector('[role="alert"]')).toBeTruthy());
    expect(option("询问").getAttribute("aria-pressed")).toBe("true");
    expect(onChanged).not.toHaveBeenCalled();
  });

  it("refreshes status after the kernel accepts the choice", async () => {
    const { open, option, onChanged } = draw();
    await open();
    await userEvent.click(option("不询问"));
    await waitFor(() => expect(onChanged).toHaveBeenCalledTimes(1));
  });
});
