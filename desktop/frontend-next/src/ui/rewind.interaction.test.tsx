// @vitest-environment jsdom
import { useState } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import type { AgentPort, RewindScope, RewindUndo } from "../port/port";
import { fromHistory } from "../state/session";
import { RewindControl } from "./cards/RewindControl";
import { useRewindActions } from "./rewind";

afterEach(cleanup);

function draw(scope: RewindScope, initialUndo: RewindUndo | null = null) {
  let available = initialUndo;
  const availableUndo = vi.fn(async () => available);
  const history = [{ role: "user" as const, content: "修改文件", msgIndex: 0 }];
  const reloadSession = vi.fn();
  const undoRewind = vi.fn(async (_transactionId: string) => {});
  const port = {
    prepareRewind: vi.fn(async () => ({ planId: "plan-0", fileCount: 1, requiresConfirmation: false })),
    availableUndo,
    commitRewind: vi.fn(async () => {
      available = { transactionId: "tx-0", turn: 0, files: 1 };
      return { ok: true, conversationOk: scope !== "code", undoAvailable: true, transactionId: "tx-0" };
    }),
    undoRewind,
  } as unknown as AgentPort;

  function Rewind() {
    const [items, setItems] = useState(() => fromHistory(history).items);
    const actions = useRewindActions(port, () => {
      reloadSession();
      setItems(fromHistory(history).items);
    });
    return <RewindControl
      key={items[0].id}
      cp={{ turn: 0, prompt: "修改文件", files: 1, msgIndex: 0 }}
      onPrepare={actions.onPrepareRewind}
      onCommit={actions.onCommitRewind}
      onReadUndo={actions.onReadUndo}
      onUndo={actions.onUndoRewind}
    />;
  }

  const view = render(<Rewind />);
  return { reloadSession, undoRewind, availableUndo, invalidate: () => { available = null; }, remount: () => { view.unmount(); render(<Rewind />); } };
}

it("reloads history after a code-only rewind and recovers undo when the menu reopens", async () => {
  const { reloadSession, undoRewind, availableUndo } = draw("code");
  await userEvent.click(screen.getByRole("button", { name: "回到这里" }));
  await userEvent.click(await screen.findByRole("menuitem", { name: /只还原代码/ }));
  await waitFor(() => expect(reloadSession).toHaveBeenCalledTimes(1));
  expect(screen.queryByRole("menuitem", { name: "撤销这次还原" })).toBeNull();
  await userEvent.click(screen.getByRole("button", { name: "回到这里" }));
  const undo = await screen.findByRole("menuitem", { name: "撤销这次还原" });
  expect(availableUndo).toHaveBeenCalledTimes(2);
  await userEvent.click(undo);
  await waitFor(() => expect(undoRewind).toHaveBeenCalledWith("tx-0"));
});

it.each([
  { scope: "conversation" as const, label: "只回退对话" },
  { scope: "both" as const, label: "代码和对话" },
])("reloads history after a $scope rewind", async ({ scope, label }) => {
  const { reloadSession } = draw(scope);
  await userEvent.click(screen.getByRole("button", { name: "回到这里" }));
  await userEvent.click(await screen.findByRole("menuitem", { name: new RegExp(label) }));
  await waitFor(() => expect(reloadSession).toHaveBeenCalledTimes(1));
});


it("reads the backend offer after closing the menu and remounting the session", async () => {
  const { availableUndo, remount, undoRewind, reloadSession } = draw("code");
  const trigger = () => screen.getByRole("button", { name: "回到这里" });
  await userEvent.click(trigger());
  await userEvent.click(await screen.findByRole("menuitem", { name: /只还原代码/ }));
  await waitFor(() => expect(reloadSession).toHaveBeenCalledTimes(1));
  await userEvent.click(trigger());
  await screen.findByRole("menuitem", { name: "撤销这次还原" });
  await userEvent.click(trigger());
  await userEvent.click(trigger());
  await screen.findByRole("menuitem", { name: "撤销这次还原" });
  remount();
  await userEvent.click(trigger());
  await userEvent.click(await screen.findByRole("menuitem", { name: "撤销这次还原" }));
  expect(availableUndo).toHaveBeenCalledTimes(4);
  expect(undoRewind).toHaveBeenCalledWith("tx-0");
});

it("does not restore an entry that the backend invalidated", async () => {
  const { invalidate } = draw("code", { transactionId: "tx-0", turn: 0, files: 1 });
  await userEvent.click(screen.getByRole("button", { name: "回到这里" }));
  await screen.findByRole("menuitem", { name: "撤销这次还原" });
  await userEvent.click(screen.getByRole("button", { name: "回到这里" }));
  invalidate();
  await userEvent.click(screen.getByRole("button", { name: "回到这里" }));
  await screen.findByRole("menuitem", { name: /只还原代码/ });
  expect(screen.queryByRole("menuitem", { name: "撤销这次还原" })).toBeNull();
});

it("does not attach another turn's undo offer to this card", async () => {
  draw("code", { transactionId: "tx-other", turn: 1, files: 1 });
  await userEvent.click(screen.getByRole("button", { name: "回到这里" }));
  await screen.findByRole("menuitem", { name: /只还原代码/ });
  expect(screen.queryByRole("menuitem", { name: "撤销这次还原" })).toBeNull();
});
