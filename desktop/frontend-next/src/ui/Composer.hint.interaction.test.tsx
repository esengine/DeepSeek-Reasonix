// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Composer } from "./Composer";
import { ComposerHintSection } from "./ComposerHintSection";
import { MockPort } from "../port/mock";
import { boot, STORAGE } from "../i18n";
import { onComposerHintChange, setShowsComposerHint, showsComposerHint } from "../state/prefs";

beforeEach(() => { localStorage.clear(); localStorage.setItem(STORAGE, "zh"); boot(); });
afterEach(() => { cleanup(); localStorage.clear(); vi.restoreAllMocks(); });

function draw(queued = false) {
  const onSubmit = vi.fn(async () => true);
  const view = render(<Composer port={new MockPort()} status={null} running={queued} queued={queued} onSubmit={onSubmit} onChanged={vi.fn()} onError={vi.fn()} />);
  return { ...view, onSubmit, box: screen.getByRole<HTMLTextAreaElement>("combobox", { name: "任务输入" }) };
}

describe("composer hint visibility", () => {
  it("defaults on, accepts only explicit off, and tolerates unavailable storage", () => {
    expect(showsComposerHint()).toBe(true);
    for (const value of ["", "on", "false", "garbage"]) {
      localStorage.setItem("rx-composer-hint", value);
      expect(showsComposerHint()).toBe(true);
    }
    setShowsComposerHint(false);
    expect(showsComposerHint()).toBe(false);
    const get = vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => { throw new Error("blocked"); });
    const set = vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => { throw new Error("blocked"); });
    expect(showsComposerHint()).toBe(true);
    expect(() => setShowsComposerHint(false)).not.toThrow();
    get.mockRestore();
    set.mockRestore();
  });

  it("notifies mounted subscribers and removes them on unsubscribe", () => {
    const heard = vi.fn();
    const stop = onComposerHintChange(heard);
    setShowsComposerHint(false);
    stop();
    setShowsComposerHint(true);
    expect(heard).toHaveBeenCalledTimes(1);
  });

  it.each([false, true])("applies immediately and persists on remount (queued=%s)", async (queued) => {
    const settings = render(<ComposerHintSection />);
    const view = draw(queued);
    expect(view.box.placeholder.length).toBeGreaterThan(0);
    await userEvent.click(within(settings.container).getByRole("button", { name: "关闭" }));
    expect(view.box.placeholder).toBe("");
    expect(view.box.getAttribute("aria-label")).toBe("任务输入");
    expect(view.box.getAttribute("aria-describedby")).toBeTruthy();
    expect(localStorage.getItem("rx-composer-hint")).toBe("off");
    view.unmount();
    const remounted = draw(queued);
    expect(remounted.box.placeholder).toBe("");
    await userEvent.click(within(settings.container).getByRole("button", { name: "显示" }));
    await waitFor(() => expect(remounted.box.placeholder.length).toBeGreaterThan(0));
    expect(localStorage.getItem("rx-composer-hint")).toBe("on");
  });

  it("keeps entered text, focus, IME confirmation and submission when hidden", async () => {
    localStorage.setItem("rx-composer-hint", "off");
    const { box, onSubmit } = draw();
    await userEvent.type(box, "implement the change");
    expect(box.value).toBe("implement the change");
    expect(document.activeElement).toBe(box);
    fireEvent.compositionStart(box);
    fireEvent.keyDown(box, { key: "Enter", isComposing: true });
    expect(onSubmit).not.toHaveBeenCalled();
    fireEvent.compositionEnd(box);
    vi.spyOn(Date, "now").mockReturnValue(Date.now() + 120);
    fireEvent.keyDown(box, { key: "Enter" });
    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith("implement the change"));
  });
});
