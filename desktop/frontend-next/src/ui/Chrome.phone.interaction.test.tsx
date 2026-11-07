// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import "./testkit";
import { Chrome } from "./Chrome";
import { RailToggle } from "./RailToggle";
import { boot, STORAGE } from "../i18n";
import { setReachable, setViewer } from "../state/viewer";
import type { SessionStatus } from "../port/session";

const PHONE = "rail side scene phone";

function atFold(fold: string) {
  document.documentElement.dataset.fold = fold;
}

const status = (over: Partial<SessionStatus> = {}) =>
  ({ workspaceRoot: "D:/work/DeepSeek-Reasonix", running: false, ...over }) as SessionStatus;

function bar(over: Partial<SessionStatus> = {}) {
  return render(
    <Chrome
      port={null} status={status(over)} workspace={null} title="并行会话演示" steer={0} account={null}
      theme="light" onTheme={() => {}} onFind={() => {}} view={"flow" as never} rows={0} surfaces={0} onView={() => {}}
    />,
  );
}

beforeEach(() => {
  localStorage.setItem(STORAGE, "zh");
  boot();
  setViewer({ id: "d1", ordinal: 1, machine: "MacBook" });
  setReachable(true);
});
afterEach(() => {
  cleanup();
  setViewer(null);
  setReachable(true);
  delete document.documentElement.dataset.fold;
  vi.restoreAllMocks();
});

const sub = () => document.querySelector<HTMLElement>(".chrome .crumb-sub");

describe("the title bar on a phone", () => {
  it("names the machine and the project under the title", () => {
    atFold(PHONE);
    bar();
    expect(sub()?.textContent).toBe("MacBook · DeepSeek-Reasonix");
    expect(sub()?.getAttribute("data-state")).toBe("ok");
  });

  it("says a turn is running, from the machine's side", () => {
    atFold(PHONE);
    bar({ running: true });
    expect(sub()?.textContent).toBe("执行中 · MacBook");
    expect(sub()?.getAttribute("data-state")).toBe("run");
  });

  it("says the machine has gone away, and which one", async () => {
    atFold(PHONE);
    bar();
    await act(async () => setReachable(false));
    expect(sub()?.textContent).toBe("MacBook · 已断开");
    expect(sub()?.getAttribute("data-state")).toBe("down");
  });

  it("gathers find, theme and the rest behind one 'more' button", () => {
    atFold(PHONE);
    bar();
    expect(screen.getByRole("button", { name: "更多" })).toBeTruthy();
  });

  it("draws nothing under the title off a paired device", () => {
    atFold(PHONE);
    setViewer(null);
    bar();
    expect(sub()).toBeNull();
  });

  it("leaves a window wider than a phone as it was", () => {
    atFold("rail side scene");
    bar();
    expect(sub()).toBeNull();
    expect(screen.queryByRole("button", { name: "更多" })).toBeNull();
  });
});

describe("the button that opens the session list", () => {
  it("is called the session list on a phone, and the workspace rail elsewhere", () => {
    atFold(PHONE);
    const view = render(<RailToggle rail={false} onRail={() => {}} />);
    expect(screen.getByRole("button", { name: "会话列表" })).toBeTruthy();
    view.unmount();
    atFold("rail side scene");
    render(<RailToggle rail={false} onRail={() => {}} />);
    expect(screen.queryByRole("button", { name: "会话列表" })).toBeNull();
  });
});
