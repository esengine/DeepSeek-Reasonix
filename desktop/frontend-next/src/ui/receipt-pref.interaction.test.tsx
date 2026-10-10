// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, renderHook, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { useFind } from "./usefind";
import { Transcript } from "./Transcript";
import { Settings } from "./Settings";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";
import { boot, STORAGE, t } from "../i18n";
import { setShowsReceipt, showsReceipt } from "../state/prefs";
import type { Item } from "../state/session";
import type { AgentPort, SessionStatus } from "../port/port";

beforeEach(() => {
  localStorage.clear();
  localStorage.setItem(STORAGE, "zh");
  boot();
});
afterEach(cleanup);

const noop = async () => undefined as never;
const user = (id: string, text: string): Item => ({ t: "user", id, text });
const say = (id: string, text: string): Item => ({ t: "say", id, text, done: true });
const receipt = (id: string): Item => ({
  t: "receipt", id,
  r: { saysSomething: true, verdict: "partial", gaps: [{ kind: "unverified_change", detail: "ruff check ." }] },
} as unknown as Item);

function transcript(items: Item[]) {
  return (
    <Transcript
      items={items} entering={[]} onEntered={() => {}} revision={1} waiting={{}} scroll={{ current: null }}
      hidden={false} onPinned={() => {}} jump={0} focus={null} onApprove={noop} onFullAccess={noop} onPlan={noop}
      onAnswer={noop} onForget={noop} onExtInvoke={() => {}} onExtSubmit={noop} checkpoints={new Map()}
      onPrepareRewind={noop} onCommitRewind={noop} onUndoRewind={noop} onPrepareFileRevert={noop}
      onCommitFileRevert={noop} needsProject={false} onOpenProject={() => {}} onKeepHere={() => {}}
    />
  );
}

const items = [user("u1", "survey"), say("s1", "surveyed"), receipt("r1")];
const card = () => document.querySelector('[data-item="r1"] .call');

describe("the delivery receipt card is a display choice", () => {
  it("is hidden until this machine turns it on, and leaves nothing drawn in its place", () => {
    expect(showsReceipt()).toBe(false);
    render(transcript(items));
    expect(card()).toBeNull();
    expect(document.querySelector('[data-item="r1"]')?.textContent ?? "").toBe("");
    expect(screen.getByText("surveyed")).toBeTruthy();
  });

  it("appears and disappears as the preference flips, on a transcript already drawn", () => {
    render(transcript(items));
    act(() => setShowsReceipt(true));
    expect(card()).not.toBeNull();
    act(() => setShowsReceipt(false));
    expect(card()).toBeNull();
  });

  it("is there on a reloaded transcript when the preference is on", () => {
    localStorage.setItem("rx-turn-receipt", "on");
    render(transcript(items));
    expect(card()).not.toBeNull();
  });

  it("keeps a saved off as off", () => {
    localStorage.setItem("rx-turn-receipt", "off");
    render(transcript(items));
    expect(card()).toBeNull();
  });

});

describe("find", () => {
  it("follows the preference instead of a cached result", () => {
    const { result } = renderHook(() => useFind(items, 1, true, () => {}));
    act(() => result.current.ask("ruff"));
    expect(result.current.total).toBe(0);
    act(() => setShowsReceipt(true));
    expect(result.current.total).toBeGreaterThan(0);
    act(() => setShowsReceipt(false));
    expect(result.current.total).toBe(0);
  });
});

describe("the setting", () => {
  it("is one switch in the session settings, off by default, and flips the card at once", async () => {
    vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(null);
    const port = new MockPort() as unknown as AgentPort;
    render(<><Settings
      hub={new MockHub() as never} port={port}
      status={{ preset: "balanced", toolApprovalMode: "ask" } as SessionStatus}
      theme="light" onTheme={() => {}} contrast="" onContrast={() => {}} weight="" onWeight={() => {}}
      look={{} as never} onLook={() => {}} reloadThemes={() => {}}
      onClose={() => {}} onChanged={() => {}} onError={() => {}} at="session"
      account={null} accountUnread="" reloadAccount={() => {}}
    /> {transcript(items)}</>);
    const sw = await screen.findByRole("switch", { name: t("显示交付验收卡片") });
    expect(sw.getAttribute("aria-checked")).toBe("false");
    expect(card()).toBeNull();
    await userEvent.click(sw);
    expect(sw.getAttribute("aria-checked")).toBe("true");
    expect(card()).not.toBeNull();
    await userEvent.click(sw);
    expect(card()).toBeNull();
  });
});
