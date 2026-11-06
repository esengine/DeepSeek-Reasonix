// @vitest-environment jsdom
import "./testkit";
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Item } from "../state/session";
import type { Checkpoint } from "../port/port";
import { UserCard } from "./cards/UserCard";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

function pointer(coarse: boolean) {
  vi.spyOn(window, "matchMedia").mockImplementation((query: string) => ({
    matches: coarse && query === "(pointer: coarse)",
    media: query,
    onchange: null,
    addListener() {},
    removeListener() {},
    addEventListener() {},
    removeEventListener() {},
    dispatchEvent: () => false,
  }) as MediaQueryList);
}

const msg = { t: "user", id: "row", text: "first" } as Extract<Item, { t: "user" }>;
const cp: Checkpoint = { turn: 4, prompt: "first", files: 0, msgIndex: 7 };

async function openRewrite() {
  const resend = vi.fn(() => Promise.resolve());
  render(<UserCard item={msg} cp={cp} onResend={resend} />);
  await userEvent.click(screen.getByRole("button", { name: /改写/ }));
  return { resend, area: screen.getByRole("textbox") as HTMLTextAreaElement };
}

describe("rewrite box", () => {
  it("Enter resends with a hardware keyboard", async () => {
    pointer(false);
    const { resend, area } = await openRewrite();
    await userEvent.type(area, "{Enter}");
    expect(resend).toHaveBeenCalledWith(4, "first");
  });

  it("Shift+Enter is a newline with a hardware keyboard", async () => {
    pointer(false);
    const { resend, area } = await openRewrite();
    await userEvent.type(area, "{Shift>}{Enter}{/Shift}more");
    expect(resend).not.toHaveBeenCalled();
    expect(area.value).toBe("first\nmore");
  });

  it("Enter is a newline on a touch device", async () => {
    pointer(true);
    const { resend, area } = await openRewrite();
    await userEvent.type(area, "{Enter}more");
    expect(resend).not.toHaveBeenCalled();
    expect(area.value).toBe("first\nmore");
  });

  it("Shift+Enter is still a newline on a touch device", async () => {
    pointer(true);
    const { resend, area } = await openRewrite();
    await userEvent.type(area, "{Shift>}{Enter}{/Shift}more");
    expect(resend).not.toHaveBeenCalled();
    expect(area.value).toBe("first\nmore");
  });

  it("the button resends the multi-line draft on a touch device", async () => {
    pointer(true);
    const { resend, area } = await openRewrite();
    await userEvent.type(area, "{Enter}more");
    await userEvent.click(screen.getByRole("button", { name: "改完重发" }));
    expect(resend).toHaveBeenCalledWith(4, "first\nmore");
  });

  it.each([false, true])("Enter during IME composition never resends (touch=%s)", async (coarse) => {
    pointer(coarse);
    const { resend, area } = await openRewrite();
    fireEvent.keyDown(area, { key: "Enter", isComposing: true });
    expect(resend).not.toHaveBeenCalled();
  });

  it.each([false, true])("Escape abandons the edit (touch=%s)", async (coarse) => {
    pointer(coarse);
    const { resend, area } = await openRewrite();
    await userEvent.type(area, "{Escape}");
    expect(screen.queryByRole("textbox")).toBeNull();
    expect(resend).not.toHaveBeenCalled();
  });

  it("an emptied draft stays unsendable on a touch device", async () => {
    pointer(true);
    const { resend, area } = await openRewrite();
    await userEvent.clear(area);
    await userEvent.type(area, "{Enter}");
    expect((screen.getByRole("button", { name: "改完重发" }) as HTMLButtonElement).disabled).toBe(true);
    expect(resend).not.toHaveBeenCalled();
  });
});
