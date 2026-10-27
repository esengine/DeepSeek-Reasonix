// @vitest-environment jsdom
import { cleanup, render, waitFor } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import "./testkit";
import { Transcript } from "./Transcript";
import { initialState, reduce, type SessionState } from "../state/session";

const noop = async () => undefined as never;
const stable = { entering: [] as string[], onEntered: () => {}, waiting: {}, scroll: { current: null }, onExtInvoke: () => {}, checkpoints: new Map(), onOpenProject: () => {}, onKeepHere: () => {}, takeovers: {} };
const view = (state: SessionState) => (
  <Transcript items={state.items} revision={state.revision} {...stable} hidden={false} focus={null}
    onApprove={noop} onFullAccess={noop} onPlan={noop} onAnswer={noop} onForget={noop} onExtSubmit={noop}
    onPrepareRewind={noop} onCommitRewind={noop} onPrepareFileRevert={noop} onCommitFileRevert={noop} needsProject={false} />
);
afterEach(cleanup);

it.each(["reasoning", "text"] as const)("keeps %s visible while guidance waits, including after withdrawal", async (kind) => {
  let state = reduce(initialState, { kind: "turn_started", msgIndex: 1 });
  state = reduce(state, { kind, text: "Before guidance. " });
  state = reduce(state, { kind: "__user", id: "pending-1", text: "Keep investigating", pending: true });
  state = reduce(state, { kind: "__queued", id: "pending-1", itemId: "inbox-1", queued: "steer" });
  state = reduce(state, { kind: "__user", id: "pending-2", text: "Also check the boundary", pending: true });
  state = reduce(state, { kind: "__queued", id: "pending-2", itemId: "inbox-2", queued: "followup" });
  const { container, rerender } = render(view(state));
  const selector = kind === "reasoning" ? ".think .tk" : ".txt";
  await waitFor(() => expect(container.querySelector(selector)?.textContent).toContain("Before guidance."));

  state = reduce(state, { kind, text: "After guidance." });
  rerender(view(state));
  await waitFor(() => expect(container.querySelector(selector)?.textContent).toContain("Before guidance. After guidance."));

  state = reduce(state, { kind: "__unsent", id: "inbox-1" });
  state = reduce(state, { kind, text: " Still streaming." });
  rerender(view(state));
  await waitFor(() => expect(container.querySelector(selector)?.textContent).toContain("Before guidance. After guidance. Still streaming."));
  expect(container.querySelectorAll('[data-k="say"]')).toHaveLength(1);
});
