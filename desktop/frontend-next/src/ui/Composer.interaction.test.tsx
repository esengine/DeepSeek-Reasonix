// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import "./testkit";
import { Composer } from "./Composer";
import { MockPort } from "../port/mock";
import type { AgentPort, ApprovalMode, Attachment, Preset, SessionStatus } from "../port/port";

afterEach(cleanup);

const status = (over: Partial<SessionStatus> = {}) =>
  ({
    preset: "balanced" as Preset,
    effort: "auto",
    toolApprovalMode: "ask" as ApprovalMode,
    plan: false,
    modelRef: "deepseek/deepseek-v4-pro",
    ...over,
  }) as SessionStatus;

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

function draw(
  over: { running?: boolean; onSubmit?: (text: string) => Promise<boolean>; port?: MockPort; st?: SessionStatus } = {},
) {
  const port = over.port ?? new MockPort();
  const onSubmit = over.onSubmit ?? vi.fn(async () => true);
  const view = render(
    <Composer
      port={port as unknown as AgentPort}
      status={over.st ?? status()}
      running={over.running ?? false}
      focus={0}
      onSubmit={onSubmit}
      onChanged={vi.fn()}
      onError={vi.fn()}
    />,
  );
  const box = view.container.querySelector('textarea[aria-label="任务输入"]') as HTMLTextAreaElement;
  return { ...view, port, onSubmit, box };
}

describe("composer submission", () => {
  it("disables a send that has nothing to send", () => {
    const { box } = draw();
    expect((screen.getByRole("button", { name: "发送" }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(box, { target: { value: "检查这次改动", selectionStart: 6 } });
    expect((screen.getByRole("button", { name: "发送" }) as HTMLButtonElement).disabled).toBe(false);
  });

  it("locks repeated Enter presses until the first submit settles", async () => {
    const pending = deferred<boolean>();
    const onSubmit = vi.fn(() => pending.promise);
    const { box } = draw({ onSubmit });
    fireEvent.change(box, { target: { value: "只发一次", selectionStart: 4 } });
    fireEvent.keyDown(box, { key: "Enter" });
    fireEvent.keyDown(box, { key: "Enter" });
    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect(box.getAttribute("aria-busy")).toBe("true");
    await act(async () => pending.resolve(true));
  });

  it("restores the exact draft and caret when the host refuses it", async () => {
    const pending = deferred<boolean>();
    const { box } = draw({ onSubmit: () => pending.promise });
    fireEvent.change(box, { target: { value: "保留这份草稿", selectionStart: 3 } });
    box.setSelectionRange(3, 3);
    fireEvent.click(screen.getByRole("button", { name: "发送" }));
    await act(async () => pending.resolve(false));
    await waitFor(() => expect(box.value).toBe("保留这份草稿"));
    expect(box.selectionStart).toBe(3);
    expect(document.activeElement).toBe(box);
  });

  it("restores a folded long paste as a chip instead of flattening it", async () => {
    const pending = deferred<boolean>();
    const { box } = draw({ onSubmit: () => pending.promise });
    const body = Array.from({ length: 81 }, (_, i) => `line ${i}`).join("\n");
    fireEvent.paste(box, { clipboardData: { files: [], getData: () => body } });
    expect(screen.getByText("81 行 · 展开到输入框")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "发送" }));
    await act(async () => pending.resolve(false));
    expect(await screen.findByText("81 行 · 展开到输入框")).toBeTruthy();
    expect(box.value).toBe("");
  });
});

describe("composer run controls", () => {
  it("offers steering and stopping as separate actions during a live turn", async () => {
    const port = new MockPort();
    const cancel = vi.spyOn(port, "cancel").mockResolvedValue();
    const onSubmit = vi.fn(async () => true);
    const { box } = draw({ port, running: true, onSubmit });
    expect((screen.getByRole("button", { name: "插话" }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByRole("button", { name: "停下" }) as HTMLButtonElement).disabled).toBe(false);
    fireEvent.change(box, { target: { value: "先检查日志", selectionStart: 5 } });
    fireEvent.click(screen.getByRole("button", { name: "插话" }));
    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith("先检查日志"));
    fireEvent.click(screen.getByRole("button", { name: "停下" }));
    await waitFor(() => expect(cancel).toHaveBeenCalledTimes(1));
  });

  it("does not take Shift+Tab away from reverse focus navigation", () => {
    const port = new MockPort();
    const plan = vi.spyOn(port, "setPlanMode");
    const { box } = draw({ port });
    fireEvent.keyDown(box, { key: "Tab", shiftKey: true });
    expect(plan).not.toHaveBeenCalled();
  });

  it("keeps stop pending until the live turn actually ends", async () => {
    const port = new MockPort();
    const cancel = vi.spyOn(port, "cancel").mockResolvedValue();
    const props = {
      port: port as unknown as AgentPort,
      status: status(),
      focus: 0,
      onSubmit: vi.fn(async () => true),
      onChanged: vi.fn(),
      onError: vi.fn(),
    };
    const view = render(<Composer {...props} running />);
    fireEvent.click(screen.getByRole("button", { name: "停下" }));
    const pending = await screen.findByRole("button", { name: "正在停止…" });
    expect((pending as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(pending);
    expect(cancel).toHaveBeenCalledTimes(1);
    expect((screen.getByRole("button", { name: "插话" }) as HTMLButtonElement).disabled).toBe(true);
    view.rerender(<Composer {...props} running={false} />);
    await waitFor(() => expect(screen.queryByRole("button", { name: "正在停止…" })).toBeNull());
  });
});

describe("composer menus", () => {
  it("dismisses completion when focus moves to a toolbar control", async () => {
    const { box } = draw();
    fireEvent.change(box, { target: { value: "@", selectionStart: 1 } });
    expect(await screen.findByRole("listbox", { name: "补全" })).toBeTruthy();
    fireEvent.blur(box);
    await waitFor(() => expect(screen.queryByRole("listbox", { name: "补全" })).toBeNull());
  });

  it("gives each composer its own completion ownership ids", async () => {
    const first = draw();
    const second = draw();
    fireEvent.change(first.box, { target: { value: "/", selectionStart: 1 } });
    fireEvent.change(second.box, { target: { value: "/", selectionStart: 1 } });
    await waitFor(() => expect(screen.getAllByRole("listbox", { name: "补全" })).toHaveLength(2));
    const ids = screen.getAllByRole("listbox", { name: "补全" }).map((node) => node.id);
    expect(new Set(ids).size).toBe(2);
    expect(first.box.getAttribute("aria-controls")).toBe(ids[0]);
    expect(second.box.getAttribute("aria-controls")).toBe(ids[1]);
  });
});

describe("composer attachments", () => {
  it("settles files independently and keeps a failed item actionable", async () => {
    const port = new MockPort();
    const attach = vi.fn(async (_blob: Blob, name: string): Promise<Attachment> => {
      if (name === "bad.txt") throw new Error("too large");
      return { path: `.reasonix/attachments/${name}`, ref: `@.reasonix/attachments/${name}`, image: false };
    });
    (port as unknown as { attach: typeof attach }).attach = attach;
    const { container } = draw({ port });
    const input = container.querySelector('input[type="file"]') as HTMLInputElement;
    fireEvent.change(input, {
      target: { files: [new File(["bad"], "bad.txt", { type: "text/plain" }), new File(["ok"], "good.txt", { type: "text/plain" })] },
    });
    expect(await screen.findByText("good.txt")).toBeTruthy();
    expect(await screen.findByRole("button", { name: "添加失败 · 重试" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "移除 bad.txt" })).toBeTruthy();
    expect((screen.getByRole("button", { name: "发送" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("keeps image routing guidance outside the horizontal attachment rail", async () => {
    const port = new MockPort();
    vi.spyOn(port, "attach").mockResolvedValue({ path: ".reasonix/attachments/shot.png", ref: "@shot.png", image: true });
    const { container } = draw({ port, st: status({ vision: false, visionDeclared: false }) });
    const input = container.querySelector('input[type="file"]') as HTMLInputElement;
    fireEvent.change(input, { target: { files: [new File(["png"], "shot.png", { type: "image/png" })] } });
    const warning = await screen.findByRole("status");
    expect(warning.className).toBe("shotwarn");
    expect(warning.closest(".shots")).toBeNull();
  });
});

it("keeps every segment after the provider in a namespaced model id", async () => {
  const { container } = draw({ st: status({ modelRef: "relay/anthropic/claude-sonnet" }) });
  // The rows are the ready signal, and a Picker renders them into the body.
    await waitFor(() => expect(document.querySelector('[data-action="model.select"]')).toBeTruthy());
  expect(container.querySelector(".turntools > .picker > .mode")?.textContent).toContain("anthropic/claude-sonnet");
});
