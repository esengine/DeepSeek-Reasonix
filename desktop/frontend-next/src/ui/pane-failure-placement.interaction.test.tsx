// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render } from "@testing-library/react";
import "./testkit";
import { Pane } from "./Pane";
import { MockPort } from "../port/mock";
import type { RuntimeView } from "../port/hub";

afterEach(cleanup);

function mount(extra: Record<string, unknown>) {
  return render(<Pane port={new MockPort()} rt={{ id: "p1", root: "/w", name: "w" } as RuntimeView}
    title="w" active visible sideHost={null} side={false} onFocus={() => {}} onReport={() => {}}
    onSessionChanged={() => {}} pulse={0} findPulse={0} onSettings={() => {}} needsProject={false}
    onOpenProject={() => {}} onKeepHere={() => {}} theme="dark" dockW={560} dockMax={880} onDockW={() => {}} {...extra} />);
}

describe("where a refused action's reason sits in the pane", () => {
  it("is in the stack above the composer, not in the corner layer the composer paints over", async () => {
    const view = mount({ failure: <div className="errbar" role="alert" data-testid="refusal">这个文件夹里有正在运行的对话</div> });
    const refusal = await view.findByTestId("refusal");
    expect(refusal.parentElement?.classList.contains("composenotes")).toBe(true);
    expect(refusal.closest(".cmpalert")).toBeNull();
  });

  it("leaves a note, which has no business over the composer, in the corner layer", async () => {
    const view = mount({ alert: <div className="errbar" data-kind="note" role="status" data-testid="note">升级了</div> });
    const note = await view.findByTestId("note");
    expect(note.closest(".cmpalert")).not.toBeNull();
    expect(note.closest(".composenotes")).toBeNull();
  });
});
