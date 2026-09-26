// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Item } from "../../state/session";
import { ApprovalCard } from "./ApprovalCard";
import { AskCard } from "./AskCard";

afterEach(cleanup);

const pending = () => new Promise<void>((resolve) => setTimeout(resolve, 20));

describe("decision cards", () => {
  it("keeps every approval action locked while the decision is in flight", async () => {
    const item = {
      t: "approval", id: "row", a: { id: "gate", tool: "bash", subject: "run checks" },
    } as Extract<Item, { t: "approval" }>;
    let release = () => {};
    const approve = vi.fn(() => new Promise<void>((resolve) => { release = resolve; }));
    render(<ApprovalCard item={item} onApprove={approve} onFullAccess={vi.fn(pending)} onPlan={vi.fn(pending)} />);

    await userEvent.click(screen.getByRole("button", { name: "允许这一次" }));
    expect(screen.getByText("正在提交…")).toBeTruthy();
    for (const button of screen.getAllByRole("button")) expect((button as HTMLButtonElement).disabled).toBe(true);
    release();
    await waitFor(() => expect((screen.getByRole("button", { name: "允许这一次" }) as HTMLButtonElement).disabled).toBe(false));
    expect(approve).toHaveBeenCalledTimes(1);
  });

  // An answer the host drops must not be offered: the card used to promise "do
  // not ask again" and send a grant that died with the session, and the grant
  // that actually writes a rule was not reachable from this window at all.
  it("offers the answers this call's host says it will honour, and no others", async () => {
    const card = (allows: { allowsSession?: boolean; allowsPersist?: boolean }) => {
      const approve = vi.fn(pending);
      const item = {
        t: "approval", id: "row", a: { id: "gate", tool: "computer_act", subject: "com.apple.Notes", ...allows },
      } as Extract<Item, { t: "approval" }>;
      const r = render(<ApprovalCard item={item} onApprove={approve} onFullAccess={vi.fn(pending)} onPlan={vi.fn(pending)} />);
      return { approve, ...r };
    };
    const names = () => screen.getAllByRole("button").map((b) => b.textContent);

    const fresh = card({});
    expect(names()).toEqual(["允许这一次", "拒绝"]);
    cleanup();

    const scoped = card({ allowsSession: true });
    expect(names()).toEqual(["允许这一次", "本会话都允许", "拒绝"]);
    await userEvent.click(screen.getByRole("button", { name: "本会话都允许" }));
    expect(scoped.approve).toHaveBeenCalledWith("row", "gate", "session");
    cleanup();

    const full = card({ allowsSession: true, allowsPersist: true });
    expect(names()).toEqual(["允许这一次", "本会话都允许", "此类操作不再询问", "拒绝"]);
    await userEvent.click(screen.getByRole("button", { name: "此类操作不再询问" }));
    expect(full.approve).toHaveBeenCalledWith("row", "gate", "always");
    void fresh;
  });

  it("puts full access behind an explicit second confirmation", async () => {
    const fullAccess = vi.fn(pending);
    const item = {
      t: "approval", id: "row", a: {
        id: "gate", tool: "bash", subject: "python - <<'PY'", reasonCode: "dynamic_bash", allowsSession: true,
      },
    } as Extract<Item, { t: "approval" }>;
    render(<ApprovalCard item={item} onApprove={vi.fn(pending)} onFullAccess={fullAccess} onPlan={vi.fn(pending)} />);

    expect(screen.getByRole("button", { name: "本会话不再询问" })).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "切换全部放行…" }));
    expect(fullAccess).not.toHaveBeenCalled();
    expect(screen.getByText("全部放行会跳过后续工具确认")).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "确认全部放行" }));
    expect(fullAccess).toHaveBeenCalledWith("row");
  });

  it("does not invent a recommendation and restores answered tab state", () => {
    const item = {
      t: "ask", id: "row", answered: [["B"]], ask: { id: "ask", questions: [
        { id: "q", header: "方向", prompt: "选哪个？", multi: false, options: [{ label: "A" }, { label: "B" }] },
      ] },
    } as Extract<Item, { t: "ask" }>;
    const { container } = render(<AskCard item={item} onAnswer={vi.fn(pending)} />);
    expect(screen.queryByText("推荐")).toBeNull();
    expect(screen.getByRole("button", { name: "B" }).getAttribute("aria-pressed")).toBe("true");
    expect(within(container).getByText(/方向：/).parentElement?.textContent).toContain("B");
  });

  it("turns a supplied Other option into the single free-text choice", async () => {
    const answer = vi.fn(pending);
    const item = {
      t: "ask", id: "row", ask: { id: "ask", questions: [
        {
          id: "city", header: "城市", prompt: "选择城市", multi: false,
          options: [{ label: "北京" }, { label: "其他（请填写城市名）", description: "输入列表之外的城市。" }],
        },
      ] },
    } as Extract<Item, { t: "ask" }>;
    render(<AskCard item={item} onAnswer={answer} />);

    expect(screen.getAllByText("其他（请填写城市名）")).toHaveLength(1);
    await userEvent.click(screen.getByRole("button", { name: /其他（请填写城市名）/ }));
    await userEvent.type(screen.getByPlaceholderText("在此填写你希望采用的方案"), "深圳");
    await userEvent.click(screen.getByRole("button", { name: "确认" }));

    expect(answer).toHaveBeenCalledWith("row", "ask", [{ questionId: "city", selected: ["深圳"] }]);
  });

  // Choosing Other puts the caret in its box and Enter answers, so a typed
  // answer never needs the mouse; Enter that confirms an IME candidate does not.
  it("answers a typed Other from the keyboard, and leaves an IME confirm alone", async () => {
    const answer = vi.fn(pending);
    const item = {
      t: "ask", id: "row", ask: { id: "ask", questions: [
        { id: "city", header: "城市", prompt: "选择城市", multi: false, options: [{ label: "北京" }] },
      ] },
    } as Extract<Item, { t: "ask" }>;
    render(<AskCard item={item} onAnswer={answer} />);

    await userEvent.click(screen.getByRole("button", { name: /其他 —— 自行填写/ }));
    const box = screen.getByPlaceholderText("在此填写你希望采用的方案");
    expect(document.activeElement).toBe(box);

    await userEvent.keyboard("shenzhen");
    fireEvent.compositionStart(box);
    fireEvent.keyDown(box, { key: "Enter", isComposing: true });
    fireEvent.compositionEnd(box);
    expect(answer).not.toHaveBeenCalled();

    const later = Date.now() + 60_000;
    const clock = vi.spyOn(Date, "now").mockReturnValue(later);
    try {
      await userEvent.keyboard("{Enter}");
    } finally {
      clock.mockRestore();
    }
    expect(answer).toHaveBeenCalledWith("row", "ask", [{ questionId: "city", selected: ["shenzhen"] }]);
  });

  it("puts the caret back in the box each time Other is chosen again", async () => {
    const item = {
      t: "ask", id: "row", ask: { id: "ask", questions: [
        { id: "city", header: "城市", prompt: "选择城市", multi: false, options: [{ label: "北京" }] },
      ] },
    } as Extract<Item, { t: "ask" }>;
    render(<AskCard item={item} onAnswer={vi.fn(pending)} />);
    const other = screen.getByRole("button", { name: /其他 —— 自行填写/ });
    const box = screen.getByRole("textbox", { name: "其他 —— 自行填写" });

    await userEvent.click(other);
    expect(document.activeElement).toBe(box);
    await userEvent.click(screen.getByRole("button", { name: /北京/ }));
    await userEvent.click(other);
    expect(document.activeElement).toBe(box);
  });

  // The pane Enter leaves becomes display:none, so focus has to follow the card
  // to the next question or the keyboard is left holding nothing.
  it("takes focus to the next question when Enter moves on", async () => {
    const item = {
      t: "ask", id: "row", ask: { id: "ask", questions: [
        { id: "city", header: "城市", prompt: "选择城市", multi: false, options: [{ label: "北京" }] },
        { id: "size", header: "规模", prompt: "选择规模", multi: false, options: [{ label: "小" }, { label: "大" }] },
      ] },
    } as Extract<Item, { t: "ask" }>;
    render(<AskCard item={item} onAnswer={vi.fn(pending)} />);

    await userEvent.click(screen.getAllByRole("button", { name: /其他 —— 自行填写/ })[0]);
    await userEvent.keyboard("shenzhen{Enter}");
    expect(screen.getByRole("tab", { name: /规模/ }).getAttribute("aria-selected")).toBe("true");
    expect(document.activeElement).toBe(screen.getByRole("button", { name: /小/ }));
  });

  // Several questions are walked, not hunted for: a single-choice pick moves on
  // by itself, a multi-choice one waits for Next, and Confirm appears only once
  // every question has an answer.
  it("steps through several questions without reaching for the tabs", async () => {
    const answer = vi.fn(pending);
    const item = {
      t: "ask", id: "row", ask: { id: "ask", questions: [
        { id: "mode", header: "方式", prompt: "怎么做", multi: false, options: [{ label: "直接改" }, { label: "先出计划" }] },
        { id: "scope", header: "范围", prompt: "改哪些", multi: true, options: [{ label: "calc.go" }, { label: "calc_test.go" }] },
        { id: "style", header: "风格", prompt: "注释", multi: false, options: [{ label: "简短" }, { label: "详细" }] },
      ] },
    } as Extract<Item, { t: "ask" }>;
    render(<AskCard item={item} onAnswer={answer} />);
    const tab = (name: string) => screen.getByRole("tab", { name: new RegExp(name) });

    expect((screen.getByRole("button", { name: "下一题（1/3）" }) as HTMLButtonElement).disabled).toBe(true);
    await userEvent.click(screen.getByRole("button", { name: /直接改/ }));
    await waitFor(() => expect(tab("范围").getAttribute("aria-selected")).toBe("true"));

    await userEvent.click(screen.getByRole("button", { name: /calc_test\.go/ }));
    expect(tab("范围").getAttribute("aria-selected")).toBe("true");
    await userEvent.click(screen.getByRole("button", { name: "下一题（2/3）" }));
    expect(tab("风格").getAttribute("aria-selected")).toBe("true");

    // The last open question offers Confirm, waiting for its answer, not a Next
    // that leads nowhere.
    expect((screen.getByRole("button", { name: "确认" }) as HTMLButtonElement).disabled).toBe(true);
    await userEvent.click(screen.getByRole("button", { name: "上一题" }));
    expect(tab("范围").getAttribute("aria-selected")).toBe("true");
    await userEvent.click(tab("风格"));
    await userEvent.click(screen.getByRole("button", { name: /简短/ }));
    await userEvent.click(screen.getByRole("button", { name: "确认" }));

    expect(answer).toHaveBeenCalledWith("row", "ask", [
      { questionId: "mode", selected: ["直接改"] },
      { questionId: "scope", selected: ["calc_test.go"] },
      { questionId: "style", selected: ["简短"] },
    ]);
  });
});
