// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Shell } from "./Shell";
import { Rules } from "./Rules";
import { Context } from "./panels/Context";
import { MockPort } from "../port/mock";
import { HttpError } from "../port/http_error";
import { SAVED_NOT_APPLIED } from "../i18n/kernel";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

const refused = (code: string) => new HttpError(409, "producer diagnostic", { code, params: { detail: "controlled failure" } });
const assertOutcome = async (unapplied: boolean) => {
  const message = await screen.findByRole(unapplied ? "status" : "alert");
  expect(message.getAttribute("data-lvl")).toBe(unapplied ? "warn" : "err");
  expect(message.textContent).toContain(unapplied ? "已保存，尚未生效" : "操作未完成");
  expect(message.textContent).not.toContain("producer diagnostic");
  return message;
};

it.each(["runtime.saved_while_running", "runtime.rebuild_failed"])("keeps the effective shell when %s reports a stored choice", async (code) => {
  const port = new MockPort();
  const before = await port.shell();
  const save = vi.spyOn(port, "saveShell").mockRejectedValue(refused(code));
  const changed = vi.fn();
  render(<Shell port={port} onChanged={changed} />);
  await userEvent.click(await screen.findByRole("radio", { name: /^PowerShell 7/ }));
  await assertOutcome(true);
  expect(screen.getByRole("radio", { name: "自动" }).getAttribute("aria-checked")).toBe("true");
  expect(screen.getByText(/当前生效/).nextElementSibling?.textContent).toContain(before.effective.path);
  expect(save).toHaveBeenCalledWith("pwsh", "");
  expect(changed).not.toHaveBeenCalled();
});

it("announces a refused shell change and lets the same choice succeed on retry", async () => {
  const port = new MockPort();
  const save = vi.spyOn(port, "saveShell").mockRejectedValueOnce(refused("shell.rejected"));
  const changed = vi.fn();
  render(<Shell port={port} onChanged={changed} />);
  const choice = await screen.findByRole("radio", { name: /^PowerShell 7/ });
  await userEvent.click(choice);
  await assertOutcome(false);
  expect(changed).not.toHaveBeenCalled();
  await userEvent.click(choice);
  await waitFor(() => expect(changed).toHaveBeenCalledOnce());
  expect(choice.getAttribute("aria-checked")).toBe("true");
  expect(save).toHaveBeenCalledTimes(2);
  expect(screen.queryByRole("alert")).toBeNull();
});

it.each(["runtime.saved_while_running", "runtime.rebuild_failed"])("keeps the displayed permission rules when %s reports a stored change", async (code) => {
  const port = new MockPort();
  const save = vi.spyOn(port, "savePermissions").mockRejectedValue(refused(code));
  const changed = vi.fn();
  render(<Rules port={port} onChanged={changed} />);
  const choice = await screen.findByLabelText("bash(go test:*) 的处理方式");
  await userEvent.selectOptions(choice, "ask");
  await assertOutcome(true);
  expect((choice as HTMLSelectElement).value).toBe("allow");
  expect(save).toHaveBeenCalledWith(expect.objectContaining({ ask: ["bash(rm:*)", "bash(go test:*)"], allow: [] }));
  expect(changed).not.toHaveBeenCalled();
});

it("announces a refused permission save and applies the same change on retry", async () => {
  const port = new MockPort();
  const save = vi.spyOn(port, "savePermissions").mockRejectedValueOnce(refused("permissions.rejected"));
  const changed = vi.fn();
  render(<Rules port={port} onChanged={changed} />);
  const choice = await screen.findByLabelText("bash(go test:*) 的处理方式");
  await userEvent.selectOptions(choice, "ask");
  await assertOutcome(false);
  expect((choice as HTMLSelectElement).value).toBe("allow");
  expect(changed).not.toHaveBeenCalled();
  await userEvent.selectOptions(choice, "ask");
  await waitFor(() => expect(changed).toHaveBeenCalledOnce());
  expect((choice as HTMLSelectElement).value).toBe("ask");
  expect(save).toHaveBeenCalledTimes(2);
  expect(screen.queryByRole("alert")).toBeNull();
});

it.each(["context.window_after_this_turn", "runtime.rebuild_failed"])("leaves the live window unchanged when %s reports the stored declaration", async (code) => {
  const port = new MockPort();
  const ctx = await port.context();
  const save = vi.spyOn(port, "setContextWindow").mockRejectedValue(refused(code));
  const onCtx = vi.fn();
  render(<Context ctx={{ ...ctx, window: 0 }} legend port={port} onCtx={onCtx} />);
  const draft = screen.getByLabelText("上下文窗口（tokens）");
  await userEvent.type(draft, "32000");
  await userEvent.click(screen.getByRole("button", { name: "保存" }));
  await assertOutcome(true);
  expect((draft as HTMLInputElement).value).toBe("32000");
  expect(save).toHaveBeenCalledWith(32000);
  expect(onCtx).not.toHaveBeenCalled();
});

it("announces a refused window declaration and keeps it available for retry", async () => {
  const port = new MockPort();
  const ctx = await port.context();
  const save = vi.spyOn(port, "setContextWindow").mockRejectedValueOnce(refused("provider.editing_disabled"));
  const onCtx = vi.fn();
  render(<Context ctx={{ ...ctx, window: 0 }} legend port={port} onCtx={onCtx} />);
  await userEvent.type(screen.getByLabelText("上下文窗口（tokens）"), "32000");
  await userEvent.click(screen.getByRole("button", { name: "保存" }));
  await assertOutcome(false);
  expect(onCtx).not.toHaveBeenCalled();
  await userEvent.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(onCtx).toHaveBeenCalledOnce());
  expect(onCtx).toHaveBeenCalledWith(expect.objectContaining({ window: 32000 }));
  expect(save).toHaveBeenCalledTimes(2);
  expect(screen.queryByRole("alert")).toBeNull();
});

it("includes a stored context window among the existing saved-not-applied codes", () => {
  expect(SAVED_NOT_APPLIED).toContain("context.window_after_this_turn");
});

it("does not tell a user that a context window cannot be saved during active work", async () => {
  const port = new MockPort();
  const ctx = await port.context();
  render(<Context ctx={{ ...ctx, window: 0 }} legend port={port} onCtx={vi.fn()} />);
  expect(screen.queryByText(/任务运行期间无法修改/)).toBeNull();
});
