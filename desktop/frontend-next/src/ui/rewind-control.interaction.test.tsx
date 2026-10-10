// @vitest-environment jsdom
import "./testkit";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { RewindControl } from "./cards/RewindControl";
import type { RewindPlan } from "../port/port";

afterEach(cleanup);
it.each(["dismiss", "unmount"])("does not commit a delayed preparation after %s", async (end) => {
  let resolve!: (plan: RewindPlan) => void;
  const prepare = vi.fn(() => new Promise<RewindPlan>((done) => { resolve = done; }));
  const commit = vi.fn(async () => ({ ok: true }));
  const view = render(<RewindControl cp={{ turn: 1, prompt: "Prompt", files: 1 }} onPrepare={prepare} onCommit={commit} />);
  fireEvent.click(screen.getByRole("button", { name: "回到这里" }));
  fireEvent.click(screen.getByRole("menuitem", { name: /代码和对话/ }));
  if (end === "unmount") view.unmount();
  else fireEvent.keyDown(window, { key: "Escape" });
  await act(async () => resolve({ planId: "plan-1", turn: 1, coverage: "full", canFiles: true, canConversation: true, fileCount: 1, requiresConfirmation: false }));
  expect(commit).not.toHaveBeenCalled();
  expect(screen.queryByRole("menu")).toBeNull();
});
