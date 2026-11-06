// @vitest-environment jsdom
import "./testkit";
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { usePresetLeave } from "./PresetLeave";
import { builtinCompositions } from "../port/composition";
import { presetChanges } from "./presetDraft";

afterEach(cleanup);

function Host({ changes, leave }: { changes: string[]; leave: () => void }) {
  const guard = usePresetLeave(true, false, changes);
  return <>
    <button onClick={() => guard.request(leave)}>back</button>
    {guard.confirmation}
  </>;
}

describe("leaving a preset with unsaved changes", () => {
  it("lists what would be lost and makes keep-editing the main action", async () => {
    const leave = vi.fn();
    render(<Host changes={["预设名称", "人设与要求", "工具 · 3 项"]} leave={leave} />);
    await userEvent.click(screen.getByText("back"));
    const dialog = await screen.findByRole("alertdialog");
    expect(dialog.textContent).toContain("放弃未保存的更改？");
    expect([...dialog.querySelectorAll(".pc-chips span")].map((c) => c.textContent)).toEqual(["预设名称", "人设与要求", "工具 · 3 项"]);
    expect(document.activeElement?.textContent).toBe("继续编辑");
    const actions = [...dialog.querySelectorAll(".preset-confirm-actions button")].map((b) => b.textContent);
    expect(actions).toEqual(["放弃更改", "继续编辑"]);
  });

  it("discards through the danger action and keeps through the main one and the close button", async () => {
    const leave = vi.fn();
    render(<Host changes={[]} leave={leave} />);
    await userEvent.click(screen.getByText("back"));
    await userEvent.click(await screen.findByRole("button", { name: "关闭" }));
    expect(screen.queryByRole("alertdialog")).toBeNull();
    expect(leave).not.toHaveBeenCalled();
    await userEvent.click(screen.getByText("back"));
    await userEvent.click(await screen.findByText("放弃更改"));
    expect(leave).toHaveBeenCalledOnce();
  });

  it("draws no chip row when nothing is itemised", async () => {
    render(<Host changes={[]} leave={() => {}} />);
    await userEvent.click(screen.getByText("back"));
    expect((await screen.findByRole("alertdialog")).querySelector(".pc-chips")).toBeNull();
  });
});

describe("what changed in a preset", () => {
  const base = () => builtinCompositions()[0];

  it("is empty for an untouched draft", () => {
    expect(presetChanges(base(), base())).toEqual([]);
  });

  it("names the parts the draft moved", () => {
    const draft = base();
    draft.name = "renamed";
    draft.persona = { ...draft.persona, prefix: "be brief" };
    draft.tools = { mode: "select", members: [{ id: "a" }, { id: "b" }, { id: "c" }] };
    const out = presetChanges(draft, base());
    expect(out).toContain("预设名称");
    expect(out).toContain("人设与要求");
    expect(out.some((c) => c.endsWith("· 3 项"))).toBe(true);
  });

  it("counts a renamed description with the name", () => {
    const draft = base();
    draft.description = "something else";
    expect(presetChanges(draft, base())).toEqual(["预设名称"]);
  });
});
