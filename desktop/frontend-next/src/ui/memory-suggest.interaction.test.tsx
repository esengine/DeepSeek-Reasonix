// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Memory } from "./Memory";
import { MemorySuggestRow } from "./MemorySuggestRow";
import { MockPort } from "../port/mock";
import type { AgentPort } from "../port/port";

afterEach(cleanup);

const held = { id: "s1", name: "gbk", description: "导出文件一律 GBK 解码", body: "Exports are GBK.", scope: "project" };

function setup() {
  const mock = new MockPort();
  mock.suggestions = [held];
  return { mock, port: mock as unknown as AgentPort };
}

it("shows a held suggestion only once the turn has ended", async () => {
  const { port } = setup();
  const { rerender } = render(<MemorySuggestRow port={port} running onError={vi.fn()} />);
  expect(screen.queryByText("导出文件一律 GBK 解码")).toBeNull();
  rerender(<MemorySuggestRow port={port} running={false} onError={vi.fn()} />);
  expect(await screen.findByText("导出文件一律 GBK 解码")).toBeTruthy();
});

it("remember accepts and removes the row", async () => {
  const { mock, port } = setup();
  const accept = vi.spyOn(mock, "acceptMemorySuggestion");
  render(<MemorySuggestRow port={port} running={false} onError={vi.fn()} />);
  await userEvent.click(await screen.findByRole("button", { name: "记住" }));
  await waitFor(() => expect(screen.queryByText("导出文件一律 GBK 解码")).toBeNull());
  expect(accept).toHaveBeenCalledWith("s1");
});

it("edit sends the changed text", async () => {
  const { mock, port } = setup();
  const accept = vi.spyOn(mock, "acceptMemorySuggestion");
  render(<MemorySuggestRow port={port} running={false} onError={vi.fn()} />);
  await userEvent.click(await screen.findByRole("button", { name: "改一下…" }));
  const box = screen.getByRole("textbox");
  await userEvent.clear(box);
  await userEvent.type(box, "GBK always");
  await userEvent.click(screen.getByRole("button", { name: "记住" }));
  await waitFor(() => expect(accept).toHaveBeenCalledWith("s1", { body: "GBK always" }));
});

it("dismiss drops the row without saving", async () => {
  const { mock, port } = setup();
  const accept = vi.spyOn(mock, "acceptMemorySuggestion");
  render(<MemorySuggestRow port={port} running={false} onError={vi.fn()} />);
  await userEvent.click(await screen.findByRole("button", { name: "不用" }));
  await waitFor(() => expect(screen.queryByText("导出文件一律 GBK 解码")).toBeNull());
  expect(accept).not.toHaveBeenCalled();
});

it("a failed save keeps the row and reports", async () => {
  const { mock, port } = setup();
  vi.spyOn(mock, "acceptMemorySuggestion").mockRejectedValue(new Error("disk"));
  const onError = vi.fn();
  render(<MemorySuggestRow port={port} running={false} onError={onError} />);
  await userEvent.click(await screen.findByRole("button", { name: "记住" }));
  await waitFor(() => expect(onError).toHaveBeenCalled());
  expect(screen.getByText("导出文件一律 GBK 解码")).toBeTruthy();
});

it("the memory page's tri-state saves the chosen mode", async () => {
  const { mock, port } = setup();
  const save = vi.spyOn(mock, "setMemoryProactive");
  render(<Memory port={port} />);
  const ask = await screen.findByRole("radio", { name: "先问我" });
  expect(ask.getAttribute("aria-checked")).toBe("true");
  await userEvent.click(screen.getByRole("radio", { name: "直接记" }));
  expect(save).toHaveBeenCalledWith("auto");
  expect(screen.getByRole("radio", { name: "直接记" }).getAttribute("aria-checked")).toBe("true");
});
