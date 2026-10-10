// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import "./testkit";
import { SendShortcut } from "./SendShortcut";
import { useSendShortcut, sendShortcut, setSendShortcut } from "../state/prefs";

afterEach(() => { cleanup(); vi.restoreAllMocks(); setSendShortcut("enter"); localStorage.clear(); });
function Reader() { return <output>{useSendShortcut()}</output>; }

describe("send shortcut setting", () => {
  it("updates subscribers immediately and survives remount", () => {
    const view = render(<><SendShortcut /><Reader /></>);
    expect(screen.getByRole("radio", { name: "Enter 发送" }).getAttribute("aria-checked")).toBe("true");
    fireEvent.click(screen.getByRole("radio", { name: "Ctrl Enter 发送" }));
    expect(screen.getByText("modifier_enter")).toBeTruthy();
    view.unmount();
    render(<SendShortcut />);
    expect(screen.getByRole("radio", { name: "Ctrl Enter 发送" }).getAttribute("aria-checked")).toBe("true");
  });
  it("normalizes unknown saved values and follows storage changes", () => {
    localStorage.setItem("rx-send-shortcut", "future");
    render(<Reader />);
    expect(screen.getByText("enter")).toBeTruthy();
    act(() => { localStorage.setItem("rx-send-shortcut", "modifier_enter"); window.dispatchEvent(new StorageEvent("storage", { key: "rx-send-shortcut" })); });
    expect(screen.getByText("modifier_enter")).toBeTruthy();
  });
  it("keeps a preference when storage is unavailable", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => { throw new Error("denied"); });
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => { throw new Error("denied"); });
    setSendShortcut("modifier_enter");
    expect(sendShortcut()).toBe("modifier_enter");
    setSendShortcut("enter");
  });
});

it("keeps the selected mode when reads work but storage writes fail", () => {
  vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => { throw new Error("quota"); });
  render(<><SendShortcut /><Reader /></>);
  fireEvent.click(screen.getByRole("radio", { name: "Ctrl Enter 发送" }));
  expect(screen.getByText("modifier_enter")).toBeTruthy();
});
