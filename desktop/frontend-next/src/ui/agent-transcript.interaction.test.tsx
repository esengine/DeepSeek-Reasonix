// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import "./testkit";
import { DeckChips, type Deck } from "./DeckChips";
import { AgentTranscript } from "./panels/AgentTranscript";
import { NestedCall } from "./cards/ToolCard";
import type { Task } from "./panels/Agents";
import type { Tool } from "../port/wire";

afterEach(cleanup);

const LONG = "line-".repeat(120);
const child = (id: string, output: string): Tool => ({ id, name: "read_file", args: '{"path":"a.go"}', output, parentId: "t1" }) as Tool;
const task = (id: string, running: boolean, name: string, children: Tool[] = []): Task =>
  ({ t: "tool", id, running, children, tool: { id, name: "task", profile: { name }, output: running ? undefined : "final report", durationMs: 4000 } }) as unknown as Task;

function Host({ tasks }: { tasks: Task[] }) {
  const [open, setOpen] = useState<Deck>("agents");
  return <DeckChips tasks={tasks} jobs={[]} open={open} onOpen={(next) => setOpen(next)}/>;
}

it("lists running and finished sub-agents as separate rows and opens the one clicked", async () => {
  render(<Host tasks={[task("a", true, "Explore"), task("b", false, "Review")]}/>);
  const rows = document.querySelectorAll<HTMLElement>(".ag");
  expect([...rows].map((r) => r.dataset.status)).toEqual(["running", "done"]);
  await userEvent.click(screen.getByRole("button", { name: "查看完整记录：Review" }));
  const dialog = screen.getByRole("dialog", { name: "子代理完整记录：Review" });
  expect(dialog.textContent).toContain("final report");
  await userEvent.keyboard("{Escape}");
  expect(screen.queryByRole("dialog", { name: "子代理完整记录：Review" })).toBeNull();
});

it("keeps the sub-agent reading after every delegate has finished", () => {
  render(<Host tasks={[task("b", false, "Review")]}/>);
  const chip = document.querySelector('[data-action="deck.agents"]')!;
  expect(chip).not.toBeNull();
  expect(chip.hasAttribute("data-live")).toBe(false);
});

it("shows the whole transcript of one sub-agent, unclipped, and closes on Escape", async () => {
  const onClose = vi.fn();
  render(<AgentTranscript task={task("b", false, "Review", [child("c1", LONG), child("c2", "short")])} onClose={onClose} />);
  const dialog = screen.getByRole("dialog");
  expect(dialog.textContent).toContain(LONG);
  expect(dialog.textContent).toContain("final report");
  await userEvent.keyboard("{Escape}");
  expect(onClose).toHaveBeenCalled();
});

it("offers show-all instead of silently cutting a long nested output", async () => {
  render(<NestedCall tool={child("c1", LONG)} />);
  expect(document.body.textContent).not.toContain(LONG);
  await userEvent.click(screen.getByRole("button", { name: "显示全部" }));
  expect(document.body.textContent).toContain(LONG);
  await userEvent.click(screen.getByRole("button", { name: "收起" }));
  expect(document.body.textContent).not.toContain(LONG);
});

it("offers no show-all for an output that fits", () => {
  render(<NestedCall tool={child("c1", "short")} />);
  expect(screen.queryByRole("button", { name: "显示全部" })).toBeNull();
});

it("moves focus into the overlay, keeps Tab inside it, and returns focus to the chip on close", async () => {
  render(<Host tasks={[task("b", false, "Review", [child("c1", "x")])]} />);
  await userEvent.click(screen.getByRole("button", { name: "查看完整记录：Review" }));
  const dialog = screen.getByRole("dialog", { name: "子代理完整记录：Review" });
  expect(document.activeElement).toBe(dialog);
  await userEvent.tab();
  await userEvent.tab();
  await userEvent.tab();
  expect(dialog.contains(document.activeElement)).toBe(true);
  await userEvent.click(screen.getByRole("button", { name: "关闭" }));
  expect(document.activeElement).toBe(document.querySelector('[data-action="deck.agents"]'));
});

it("closes quietly when the viewed task is gone", async () => {
  const { rerender } = render(<Host tasks={[task("b", false, "Review")]} />);
  await userEvent.click(screen.getByRole("button", { name: "查看完整记录：Review" }));
  rerender(<Host tasks={[task("z", false, "Other")]} />);
  expect(screen.queryByRole("dialog", { name: "子代理完整记录：Review" })).toBeNull();
});

it("caps what the overlay draws and says so", () => {
  render(<AgentTranscript task={task("b", false, "Review", [child("c1", "y".repeat(250_000))])} onClose={() => {}} />);
  expect(screen.getByRole("dialog").textContent).toContain("仅显示前 200000 个字符");
});

it("counts only running delegates, reaching 0 when the last one finishes", () => {
  const n = (ts: Task[]) => render(<Host tasks={ts} />).container.querySelector('[data-action="deck.agents"] b')?.textContent;
  expect(n([task("a", true, "A"), task("b", false, "B")])).toBe("1");
  cleanup();
  expect(n([task("a", false, "A"), task("b", false, "B")])).toBe("0");
});

it("heads a refused delegation as not run and a cut-short one as interrupted", () => {
  const refused = { ...task("r", false, "explore"), tool: { id: "r", name: "task", profile: { name: "explore" }, err: "invalid arguments for explore", refusalCode: "tool.arguments_invalid" } } as unknown as Task;
  const { rerender } = render(<AgentTranscript task={refused} onClose={() => {}} />);
  const head = () => screen.getByRole("dialog").querySelector(".agent-tx-hd .rt")!.textContent;
  expect(head()).toContain("未执行");
  expect(head()).not.toContain("已中断");
  const cut = { ...refused, tool: { ...refused.tool, refusalCode: undefined, err: "sub-agent: context canceled" } } as unknown as Task;
  rerender(<AgentTranscript task={cut} onClose={() => {}} />);
  expect(head()).toContain("已中断");
});
