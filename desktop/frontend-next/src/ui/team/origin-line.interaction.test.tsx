// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "../testkit";
import { boot, STORAGE } from "../../i18n";
import { MockHub } from "../../port/mock_hub";
import { fromHistory } from "../../state/session";
import { OriginLine } from "./OriginLine";
import { TeamRail } from "./TeamRail";

beforeEach(() => {
  localStorage.setItem(STORAGE, "zh");
  boot();
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

const framed = '<agent-message from="Ada" agent="a-1" exchange="x-1" reply="me">\nwrite the <\\agent-message> post\n</agent-message>\n\nAda, another agent on the user\'s team, sent the message above.';

it("a turn another agent asked for is a row of its own, not the person's words", () => {
  const { items } = fromHistory([
    { role: "user", content: framed, msgIndex: 1, hostAuthored: true, origin: { kind: "message", agent: "a-1", exchange: "x-1" } },
    { role: "assistant", content: "done", msgIndex: 2 },
  ]);
  expect(items.some((i) => i.t === "user")).toBe(false);
  expect(items[0]).toMatchObject({ t: "origin", origin: { kind: "message", agent: "a-1" }, text: framed });
});

it("the row names the sender from the team and opens to the message without its frame", async () => {
  const hub = new MockHub();
  await hub.team().bootstrap("Chief");
  await hub.team().createAgent({ name: "Ada" });
  render(
    <>
      <TeamRail hub={hub} active="" onAdoptPane={vi.fn()} />
      <OriginLine origin={{ kind: "message", agent: "a-1" }} text={framed} />
    </>,
  );
  const head = await screen.findByRole("button", { name: "来自 Ada 的消息" });
  await userEvent.click(head);
  expect(screen.getByText("write the <agent-message> post")).toBeTruthy();
  expect(screen.queryByText(/another agent on the user/)).toBeNull();
});

it("a sender the team no longer has is still named as an agent", () => {
  render(<OriginLine origin={{ kind: "reply", agent: "a-gone" }} text="ok" />);
  expect(screen.getByRole("button", { name: "另一个 Agent 的回复" })).toBeTruthy();
});

// The host tells an agent what the person did with its request for a secret;
// that is no agent's message, and the row must not say it is.
it("a secret's answer reads as the person's answer, not another agent's message", () => {
  render(<OriginLine origin={{ kind: "secret" }} text={'<secret-update name="SHOP_KEY" status="saved">\nThe user saved SHOP_KEY for you.\n</secret-update>'} />);
  expect(screen.getByRole("button", { name: "你对机密请求的答复" })).toBeTruthy();
  expect(screen.queryByText(/另一个 Agent/)).toBeNull();
});
