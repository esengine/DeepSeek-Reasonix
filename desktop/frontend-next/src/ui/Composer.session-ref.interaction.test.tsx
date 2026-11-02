// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import "./testkit";
import { Composer } from "./Composer";
import { MockPort } from "../port/mock";
import type { Completion, SessionStatus } from "../port/port";

afterEach(() => { cleanup(); localStorage.clear(); vi.restoreAllMocks(); });

const past = [
  { label: "Rotate the signing key", insert: "@session:20261018-020806-test", hint: "2026-10-18 02:08 · 4", kind: "session" },
  { label: "Fix the flaky branch menu", insert: "@session:20261017-101010-test", hint: "2026-10-17 10:10 · 9", kind: "session" },
];

function draw() {
  const port = new MockPort();
  vi.spyOn(port, "complete").mockImplementation(async (line, cursor): Promise<Completion> => {
    const at = line.lastIndexOf("#", cursor);
    return at < 0 ? { kind: "", from: 0, to: 0, items: [] } : { kind: "session", from: at, to: line.length, query: line.slice(at + 1), items: past };
  });
  const onSubmit = vi.fn(async () => true);
  const view = render(<Composer
    port={port}
    status={{ plan: false, effort: "auto", modelRef: "deepseek/deepseek-v4-pro" } as SessionStatus}
    running={false} focus={0} onSubmit={onSubmit} onChanged={vi.fn()} onError={vi.fn()}
  />);
  const box = screen.getByRole("combobox", { name: "任务输入" }) as HTMLTextAreaElement;
  const type = (value: string, caret = value.length) => fireEvent.change(box, { target: { value, selectionStart: caret } });
  return { ...view, port, onSubmit, box, type };
}

it("turns a picked past conversation into a removable chip and takes the typed word out of the box", async () => {
  const { box, type } = draw();
  type("continue from #sig");
  const option = await screen.findByRole("option", { name: /Rotate the signing key/ });
  expect(screen.getAllByText("会话")).toHaveLength(2);
  fireEvent.mouseDown(option);
  expect(box.value).toBe("continue from ");
  expect(screen.getByText("Rotate the signing key")).toBeTruthy();
  expect(screen.getByText("历史会话 · 只读资料")).toBeTruthy();
  await waitFor(() => expect(screen.queryByRole("listbox", { name: "补全" })).toBeNull());
  fireEvent.click(screen.getByRole("button", { name: "移除 Rotate the signing key" }));
  expect(screen.queryByText("Rotate the signing key")).toBeNull();
});

it("sends the reference with the typed text, ahead of it like an attachment, and only once per conversation", async () => {
  const { box, type, onSubmit } = draw();
  type("#sig");
  fireEvent.mouseDown(await screen.findByRole("option", { name: /Rotate the signing key/ }));
  type("#fix");
  fireEvent.mouseDown(await screen.findByRole("option", { name: /Rotate the signing key/ }));
  expect(screen.getAllByText("Rotate the signing key")).toHaveLength(1);
  type("what did we decide");
  fireEvent.keyDown(box, { key: "Enter" });
  await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
  expect((onSubmit.mock.calls[0] as unknown[])[0]).toBe("@session:20261018-020806-test what did we decide");
  await waitFor(() => expect(screen.queryByText("Rotate the signing key")).toBeNull());
});

it("sends a chip on its own, and puts it back with the draft when the send fails", async () => {
  const { port, box, type, onSubmit } = draw();
  onSubmit.mockResolvedValueOnce(false);
  type("#");
  fireEvent.mouseDown(await screen.findByRole("option", { name: /Fix the flaky branch menu/ }));
  fireEvent.keyDown(box, { key: "Enter" });
  await waitFor(() => expect(onSubmit).toHaveBeenCalled());
  expect((onSubmit.mock.calls[0] as unknown[])[0]).toBe("@session:20261017-101010-test");
  await waitFor(() => expect(screen.getByText("Fix the flaky branch menu")).toBeTruthy());
  expect(port.complete).toHaveBeenCalled();
});
