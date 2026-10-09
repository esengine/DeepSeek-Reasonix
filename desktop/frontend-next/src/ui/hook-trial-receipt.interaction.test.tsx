// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Hooks } from "./Hooks";
import { MockPort } from "../port/mock";
import type { AgentPort, HookDryRun } from "../port/port";

afterEach(cleanup);

function pending() {
  let resolve!: (result: HookDryRun) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<HookDryRun>((ok, fail) => { resolve = ok; reject = fail; });
  return { promise, resolve, reject };
}

async function fixture() {
  const port = new MockPort() as unknown as AgentPort;
  const original = (await port.hooks()).hooks;
  await port.saveHooks("user", [
    { ...original[1], command: "exit 2; printf first" },
    { ...original[0], command: "printf second" },
  ]);
  const rules = (await port.hooks()).hooks;
  const result = await port.dryRunHook(rules[0]);
  const trial = vi.spyOn(port, "dryRunHook");
  const save = vi.spyOn(port, "saveHooks");
  const changed = vi.fn();
  const view = render(<Hooks port={port} onChanged={changed} />);
  await userEvent.click(await screen.findByRole("button", { name: /手动添加/ }));
  return { port, rules, result, trial, save, changed, view };
}

function rows() {
  return Array.from(document.querySelectorAll<HTMLElement>(".expert .rule"));
}

function command(index = 0) {
  return rows()[index].querySelector<HTMLInputElement>(".cmd")!;
}

function run(index = 0) {
  return rows()[index].querySelector<HTMLButtonElement>('[data-action="hooks.run-once"]')!;
}

function receipt(index = 0) {
  return rows()[index].querySelector(".dryrun");
}

async function complete(index = 0) {
  await userEvent.click(run(index));
  await waitFor(() => expect(receipt(index)).toBeTruthy());
}

it.each(["command", "event", "match"] as const)("hides a completed receipt after its %s changes", async (field) => {
  const { trial, rules } = await fixture();
  await complete();
  expect(receipt()?.textContent).toContain("这一次它会挡住这一步");
  if (field === "command") fireEvent.change(command(), { target: { value: "printf untested" } });
  else if (field === "event") fireEvent.change(within(rows()[0]).getByRole("combobox"), { target: { value: "PostToolUse" } });
  else fireEvent.change(rows()[0].querySelector(".match")!, { target: { value: "write_file" } });
  expect(receipt()).toBeNull();
  expect(trial).toHaveBeenCalledExactlyOnceWith(rules[0]);
});

it("does not reuse a receipt after editing and returning to the previous command", async () => {
  const { rules, trial } = await fixture();
  await complete();
  fireEvent.change(command(), { target: { value: "printf untested" } });
  fireEvent.change(command(), { target: { value: rules[0].command } });
  expect(receipt()).toBeNull();
  expect(trial).toHaveBeenCalledExactlyOnceWith(rules[0]);
});

it("preserves another unchanged rule's receipt while a different rule is edited", async () => {
  const { trial, rules } = await fixture();
  await complete(1);
  expect(receipt(1)?.textContent).toContain("将放行");
  fireEvent.change(command(), { target: { value: "printf edited first" } });
  expect(receipt(1)?.textContent).toContain("将放行");
  expect(receipt()).toBeNull();
  expect(trial).toHaveBeenCalledExactlyOnceWith(rules[1]);
});

it("does not move a removed rule's receipt to the next rule at that index", async () => {
  const { rules, save, changed, trial } = await fixture();
  await complete();
  await userEvent.click(rows()[0].querySelector<HTMLButtonElement>('[data-action="hooks.remove"]')!);
  await waitFor(() => expect(rows()).toHaveLength(1));
  expect(command().value).toBe(rules[1].command);
  expect(receipt()).toBeNull();
  expect(save).toHaveBeenCalledExactlyOnceWith("user", [rules[1]]);
  expect(changed).toHaveBeenCalledOnce();
  expect(trial).toHaveBeenCalledExactlyOnceWith(rules[0]);
});

it("retains an unchanged receipt across a same-port rerender and editor reopen", async () => {
  const { port, changed, view, trial, rules } = await fixture();
  await complete();
  view.rerender(<Hooks port={port} onChanged={changed} />);
  expect(receipt()?.textContent).toContain("这一次它会挡住这一步");
  await userEvent.click(screen.getByRole("button", { name: /收起/ }));
  expect(rows()).toHaveLength(0);
  await userEvent.click(screen.getByRole("button", { name: /手动添加/ }));
  expect(receipt()?.textContent).toContain("这一次它会挡住这一步");
  expect(trial).toHaveBeenCalledExactlyOnceWith(rules[0]);
});

it("keeps an unchanged receipt in its original scope", async () => {
  const { rules, trial } = await fixture();
  await complete();
  await userEvent.click(screen.getByRole("radio", { name: /这个项目/ }));
  expect(rows()).toHaveLength(0);
  await userEvent.click(screen.getByRole("button", { name: "添加规则" }));
  expect(receipt()).toBeNull();
  await userEvent.click(screen.getByRole("radio", { name: /我的/ }));
  expect(receipt()?.textContent).toContain("这一次它会挡住这一步");
  expect(trial).toHaveBeenCalledExactlyOnceWith(rules[0]);
});

it("replaces the previous receipt with pending state until the next trial succeeds", async () => {
  const { result, trial, rules } = await fixture();
  await complete();
  const gate = pending();
  trial.mockImplementationOnce(() => gate.promise);
  await userEvent.click(run());
  expect(run().disabled).toBe(true);
  expect(receipt()).toBeNull();
  await act(async () => gate.resolve({ ...result, stdout: "new receipt", stderr: "" }));
  expect(receipt()?.textContent).toContain("new receipt");
  expect(run().disabled).toBe(false);
  expect(trial.mock.calls).toEqual([[rules[0]], [rules[0]]]);
});

it("does not retain a previous success after a failed retry, then retries the exact rule", async () => {
  const { trial, rules } = await fixture();
  await complete();
  const gate = pending();
  trial.mockImplementationOnce(() => gate.promise);
  await userEvent.click(run());
  await act(async () => gate.reject(new Error("trial refused")));
  expect(screen.getByText("trial refused")).toBeTruthy();
  expect(receipt()).toBeNull();
  expect(run().disabled).toBe(false);
  await complete();
  expect(screen.queryByText("trial refused")).toBeNull();
  expect(receipt()?.textContent).toContain("这一次它会挡住这一步");
  expect(trial.mock.calls).toEqual([[rules[0]], [rules[0]], [rules[0]]]);
});

it("hides an old failure after its rule is edited", async () => {
  const { trial, rules } = await fixture();
  trial.mockRejectedValueOnce(new Error("old command refused"));
  await userEvent.click(run());
  expect(screen.getByText("old command refused")).toBeTruthy();
  fireEvent.change(command(), { target: { value: "printf untested" } });
  expect(screen.queryByText("old command refused")).toBeNull();
  expect(receipt()).toBeNull();
  expect(trial).toHaveBeenCalledExactlyOnceWith(rules[0]);
});

it.each(["success", "failure"] as const)("keeps a newer completed trial when an earlier trial settles with %s", async (outcome) => {
  const { trial, rules, result } = await fixture();
  const first = pending();
  const other = pending();
  const latest = pending();
  trial.mockImplementationOnce(() => first.promise).mockImplementationOnce(() => other.promise).mockImplementationOnce(() => latest.promise);
  await userEvent.click(run());
  await userEvent.click(run(1));
  await userEvent.click(run());
  expect(trial.mock.calls).toEqual([[rules[0]], [rules[1]], [rules[0]]]);
  await act(async () => latest.resolve({ ...result, stdout: "latest receipt", stderr: "" }));
  expect(receipt()?.textContent).toContain("latest receipt");
  await act(async () => outcome === "success"
    ? first.resolve({ ...result, stdout: "older receipt", stderr: "" })
    : first.reject(new Error("older trial refused")));
  expect(receipt()?.textContent).toContain("latest receipt");
  expect(screen.queryByText("older trial refused")).toBeNull();
  await act(async () => other.resolve({ ...result, blocks: false, decision: "pass", stdout: "other receipt", stderr: "" }));
  expect(receipt(1)?.textContent).toContain("other receipt");
  expect(receipt()?.textContent).toContain("latest receipt");
});
