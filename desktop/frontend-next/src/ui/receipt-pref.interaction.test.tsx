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
import { loadReceiptPref, receiptPrefOf, showsReceipt } from "../state/receiptpref";
import { ReceiptPrefContext } from "../state/foldpref";
const adoptReceiptPref = (on: boolean) => receiptPrefOf().adopt(on ? "on" : "off");
import type { Item } from "../state/session";
import type { AgentPort, SessionStatus } from "../port/port";

beforeEach(() => {
  adoptReceiptPref(false);
  localStorage.clear();
  localStorage.setItem(STORAGE, "zh");
  boot();
});
afterEach(cleanup);

const noop = async () => undefined as never;
const user = (id: string, text: string): Item => ({ t: "user", id, text });
const say = (id: string, text: string): Item => ({ t: "say", id, text, done: true });
const receipt = (id: string, extra: Record<string, unknown> = {}): Item => ({
  t: "receipt", id,
  r: { saysSomething: true, verdict: "partial", severity: "warn", changes: [{ path: "a.py", reviewed: false }], gaps: [{ kind: "unverified_change", detail: "ruff check ." }], ...extra },
} as unknown as Item);
const hint = () => document.querySelector('[data-item="r1"] .rc');

function transcript(items: Item[]) {
  return (
    <Transcript
      items={items} entering={[]} onEntered={() => {}} revision={1} waiting={{}} scroll={{ current: null }}
      hidden={false} focus={null} onApprove={noop} onFullAccess={noop} onPlan={noop}
      onAnswer={noop} onForget={noop} onExtInvoke={() => {}} onExtSubmit={noop} checkpoints={new Map()}
      onPrepareRewind={noop} onCommitRewind={noop} onPrepareFileRevert={noop}
      onCommitFileRevert={noop} needsProject={false} onOpenProject={() => {}} onKeepHere={() => {}}
    />
  );
}

const items = [user("u1", "survey"), say("s1", "surveyed"), receipt("r1")];
const card = () => document.querySelector('[data-item="r1"] .call');

describe("the delivery receipt card is a display choice", () => {
  it("is hidden until this machine turns it on, and the kernel's grade stands in as one line", () => {
    expect(showsReceipt()).toBe(false);
    render(transcript(items));
    expect(card()).toBeNull();
    expect(hint()?.getAttribute("data-severity")).toBe("warn");
    expect(hint()?.textContent).toBe("!" + t("chat.unverified_changes"));
    expect(screen.getByRole("button", { name: t("chat.warning") + "：" + t("chat.unverified_changes") })).toBeTruthy();
    expect(screen.getByText("surveyed")).toBeTruthy();
  });

  it("says nothing when the kernel graded nothing", () => {
    render(transcript([user("u1", "survey"), say("s1", "surveyed"), receipt("r1", { severity: undefined, gaps: [] })]));
    expect(card()).toBeNull();
    expect(document.querySelector('[data-item="r1"]')?.textContent ?? "").toBe("");
  });

  it("does not call declared caveats unverified changes, even when files changed and passed", () => {
    render(transcript([user("u1", "survey"), say("s1", "surveyed"), receipt("r1", { severity: "notice", gaps: [], risks: ["one-way migration"] })]));
    expect(hint()?.getAttribute("data-severity")).toBe("notice");
    expect(hint()?.textContent).toContain(t("chat.this_turn_declared_risks_or"));
    expect(hint()?.textContent).not.toContain(t("chat.unverified_changes"));
  });

  it("opens the card on click and closes it again, saying what the click does", async () => {
    render(transcript(items));
    const line = screen.getByRole("button", { name: /未验证|unverified/i });
    expect(line.getAttribute("title")).toBe(t("chat.click_to_see_the_delivery"));
    expect(line.getAttribute("aria-expanded")).toBe("false");
    await userEvent.click(line);
    expect(card()).not.toBeNull();
    await userEvent.click(screen.getByRole("button", { name: /未验证|unverified/i }));
    expect(card()).toBeNull();
  });

  it("is the card or the line, never both", () => {
    render(transcript(items));
    act(() => adoptReceiptPref(true));
    expect(card()).not.toBeNull();
    expect(hint()).toBeNull();
    act(() => adoptReceiptPref(false));
    expect(card()).toBeNull();
    expect(hint()).not.toBeNull();
  });

  it("appears and disappears as the preference flips, on a transcript already drawn", () => {
    render(transcript(items));
    act(() => adoptReceiptPref(true));
    expect(card()).not.toBeNull();
    act(() => adoptReceiptPref(false));
    expect(card()).toBeNull();
  });

  it("is there on a transcript drawn after the preference is read as on", () => {
    adoptReceiptPref(true);
    render(transcript(items));
    expect(card()).not.toBeNull();
  });
});

describe("find", () => {
  it("follows the preference instead of a cached result", () => {
    const { result } = renderHook(() => useFind(items, 1, true, () => {}));
    act(() => result.current.ask("ruff"));
    expect(result.current.total).toBe(0);
    act(() => adoptReceiptPref(true));
    expect(result.current.total).toBeGreaterThan(0);
    act(() => adoptReceiptPref(false));
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
      onClose={() => {}} onChanged={() => {}} onError={() => {}} at="display"
      account={null} accountUnread="" reloadAccount={() => {}}
    /> <ReceiptPrefContext.Provider value={receiptPrefOf(port)}>{transcript(items)}</ReceiptPrefContext.Provider></>);
    const sw = await screen.findByRole("switch", { name: t("settings.show_the_delivery_receipt_card") });
    expect(sw.getAttribute("aria-checked")).toBe("false");
    expect(card()).toBeNull();
    await userEvent.click(sw);
    expect(sw.getAttribute("aria-checked")).toBe("true");
    expect(card()).not.toBeNull();
    expect((await port.receiptSettings()).show).toBe(true);
    await userEvent.click(sw);
    expect(card()).toBeNull();
    expect((await port.receiptSettings()).show).toBe(false);
  });

  it("puts the switch back and says why when the kernel refuses the save", async () => {
    vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(null);
    const mock = new MockPort();
    vi.spyOn(mock, "saveReceiptSettings").mockRejectedValue(new Error("disk full"));
    const port = mock as unknown as AgentPort;
    render(<Settings
      hub={new MockHub() as never} port={port}
      status={{ preset: "balanced", toolApprovalMode: "ask" } as SessionStatus}
      theme="light" onTheme={() => {}} contrast="" onContrast={() => {}} weight="" onWeight={() => {}}
      look={{} as never} onLook={() => {}} reloadThemes={() => {}}
      onClose={() => {}} onChanged={() => {}} onError={() => {}} at="display"
      account={null} accountUnread="" reloadAccount={() => {}}
    />);
    const sw = await screen.findByRole("switch", { name: t("settings.show_the_delivery_receipt_card") });
    await userEvent.click(sw);
    expect(await screen.findByText(/disk full/)).toBeTruthy();
    expect(sw.getAttribute("aria-checked")).toBe("false");
  });
});

describe("the setting lives in the kernel", () => {
  it("is read from the kernel's answer", async () => {
    const port = new MockPort();
    await port.saveReceiptSettings(true);
    await loadReceiptPref(port, true);
    expect(showsReceipt(port)).toBe(true);
  });

  it("carries an old on from this browser to the kernel once, then drops the old key", async () => {
    localStorage.setItem("rx-turn-receipt", "on");
    const port = new MockPort();
    await loadReceiptPref(port, true);
    expect((await port.receiptSettings()).show).toBe(true);
    expect(showsReceipt(port)).toBe(true);
    expect(localStorage.getItem("rx-turn-receipt")).toBeNull();
  });

  it("drops an old off without writing anything: off is the kernel's default", async () => {
    localStorage.setItem("rx-turn-receipt", "off");
    const port = new MockPort();
    const save = vi.spyOn(port, "saveReceiptSettings");
    await loadReceiptPref(port, true);
    expect(save).not.toHaveBeenCalled();
    expect(showsReceipt(port)).toBe(false);
    expect(localStorage.getItem("rx-turn-receipt")).toBeNull();
  });

  it("does not undo a kernel choice that is already on with an old off", async () => {
    localStorage.setItem("rx-turn-receipt", "off");
    const port = new MockPort();
    await port.saveReceiptSettings(true);
    await loadReceiptPref(port, true);
    expect(showsReceipt(port)).toBe(true);
  });

  it("takes the old local choice only for this machine's own kernel", async () => {
    localStorage.setItem("rx-turn-receipt", "on");
    const remote = new MockPort();
    await loadReceiptPref(remote, false);
    expect((await remote.receiptSettings()).show).toBe(false);
    expect(localStorage.getItem("rx-turn-receipt")).toBe("on");
    const local = new MockPort();
    await loadReceiptPref(local, true);
    expect((await local.receiptSettings()).show).toBe(true);
    expect(localStorage.getItem("rx-turn-receipt")).toBeNull();
  });

  it("keeps one answer per kernel instead of the last reader's", async () => {
    const a = new MockPort();
    const b = new MockPort();
    await a.saveReceiptSettings(true);
    await loadReceiptPref(a, false);
    await loadReceiptPref(b, false);
    expect(showsReceipt(a)).toBe(true);
    expect(showsReceipt(b)).toBe(false);
  });

  it("draws nothing for a receipt until its kernel has answered", async () => {
    const port = new MockPort();
    const pref = receiptPrefOf(port);
    expect(pref.state()).toBe("unknown");
    render(<ReceiptPrefContext.Provider value={pref}>{transcript(items)}</ReceiptPrefContext.Provider>);
    expect(document.querySelector('[data-item="r1"]')?.textContent ?? "").toBe("");
    await port.saveReceiptSettings(true);
    await act(async () => { await loadReceiptPref(port, false); });
    expect(card()).not.toBeNull();
  });

  it("stays hidden when the kernel cannot be read", async () => {
    const port = new MockPort();
    vi.spyOn(port, "receiptSettings").mockRejectedValue(new Error("down"));
    await loadReceiptPref(port, true);
    expect(showsReceipt(port)).toBe(false);
  });
});
