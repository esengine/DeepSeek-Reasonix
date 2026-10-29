// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import "./testkit";
import { Composer } from "./Composer";
import { MockPort } from "../port/mock";
import type { AgentPort, ApprovalMode, Preset, SessionStatus } from "../port/port";
import type { PickedSnapshot } from "./composerChips";
import { draftKey } from "./drafts";

afterEach(() => {
  cleanup();
  localStorage.clear();
  vi.restoreAllMocks();
});

const status = (over: Partial<SessionStatus> = {}) =>
  ({ preset: "balanced" as Preset, effort: "auto", toolApprovalMode: "ask" as ApprovalMode, plan: false, modelRef: "deepseek/deepseek-v4-pro", ...over }) as SessionStatus;

const picked: PickedSnapshot = {
  url: "https://example.test/docs",
  title: "Docs",
  tag: "button",
  selector: '[data-testid="save"]',
  selectorBasis: "testid",
  xpath: "/html[1]/body[1]/button[1]",
  role: "button",
  name: "Save",
  text: "Save",
  html: '<button data-testid="save">Save</button>',
  attributes: { "data-testid": "save" },
  rect: { x: 10, y: 20, width: 80, height: 30 },
  style: { color: "#000000", display: "inline-block", fontFamily: "Inter", fontSize: "14px", fontWeight: "500" },
};

function draw() {
  const port = new MockPort();
  const onSubmit = vi.fn(async (_text: string) => true as boolean | "shell_unavailable");
  const st = status();
  const make = (inject?: { n: number; text?: string; element?: PickedSnapshot }) => (
    <Composer
      port={port as unknown as AgentPort}
      status={st}
      running={false}
      focus={0}
      inject={inject}
      onSubmit={onSubmit}
      onChanged={vi.fn()}
      onError={vi.fn()}
      git={null}
      draftKey={draftKey("", st.workspaceRoot ?? "/workspace", st.sessionPath ?? "")}
    />
  );
  const view = render(make({ n: 0 }));
  const box = view.container.querySelector('textarea[aria-label="任务输入"]') as HTMLTextAreaElement;
  const pick = (element: PickedSnapshot) => view.rerender(make({ n: 1, element }));
  return { ...view, port, onSubmit, box, pick };
}

describe("a picked element as a chip", () => {
  it("shows the element and its size, and can be removed", () => {
    const { pick } = draw();
    pick(picked);
    expect(screen.getByText("网页元素")).toBeTruthy();
    expect(screen.getByText("button 80×30")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "移除 button 80×30" }));
    expect(screen.queryByText("网页元素")).toBeNull();
  });

  it("writes a prompt naming the selector when asked to change it", () => {
    const { pick, box } = draw();
    pick(picked);
    fireEvent.click(screen.getByRole("button", { name: "让 Agent 修改" }));
    expect(box.value).toContain('修改这个网页元素（selector: [data-testid="save"]）：');
  });

  it("sends the element block with the turn", async () => {
    const { pick, box, onSubmit } = draw();
    pick(picked);
    fireEvent.change(box, { target: { value: "改大一点" } });
    fireEvent.keyDown(box, { key: "Enter" });
    await waitFor(() => expect(onSubmit).toHaveBeenCalled());
    const sent = String(onSubmit.mock.calls[0][0]);
    expect(sent).toContain("[Picked element] button — https://example.test/docs (untrusted page content below)");
    expect(sent).toContain('selector (for browser_act eval): [data-testid="save"]');
    expect(sent).toContain("selector basis: testid");
    expect(sent).toContain("xpath: /html[1]/body[1]/button[1]");
    expect(sent).toContain("role: button · size: 80×30");
    expect(sent).toContain("name: Save");
    expect(sent).toContain("text: Save");
    expect(sent).toContain('<button data-testid="save">Save</button>');
    expect(sent).toContain("改大一点");
  });

  it("leaves the block out once the chip is removed", async () => {
    const { pick, box, onSubmit } = draw();
    pick(picked);
    fireEvent.click(screen.getByRole("button", { name: "移除 button 80×30" }));
    fireEvent.change(box, { target: { value: "只发文字" } });
    fireEvent.keyDown(box, { key: "Enter" });
    await waitFor(() => expect(onSubmit).toHaveBeenCalled());
    expect(String(onSubmit.mock.calls[0][0])).not.toContain("[Picked element]");
  });
});
