// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "../testkit";
import { ApprovalCard } from "./ApprovalCard";
import { boot, STORAGE } from "../../i18n";

beforeEach(() => {
  localStorage.setItem(STORAGE, "zh");
  boot();
  document.documentElement.dataset.fold = "rail side scene phone";
});
afterEach(() => {
  cleanup();
  delete document.documentElement.dataset.fold;
});

const card = (over: Record<string, unknown> = {}) =>
  render(
    <ApprovalCard
      item={{ t: "approval", id: "i1", a: { id: "a1", tool: "bash", subject: "go test ./internal/retry/...", allowsSession: true, allowsPersist: true, ...over } } as never}
      onApprove={async () => {}} onFullAccess={async () => {}} onPlan={async () => {}}
    />,
  ).container;

const footer = (box: HTMLElement) => box.querySelector<HTMLElement>(".apv-ft")!;

// On a phone the card is two big buttons and a way to the rest, so a thumb has
// one obvious thing to press; the rest stays one tap away rather than gone.
describe("the approval card's way to its other choices", () => {
  it("offers 'more options' when the card has choices beyond allow and refuse", async () => {
    const box = card();
    const more = screen.getByRole("button", { name: "更多选项…" });
    expect(more.getAttribute("aria-expanded")).toBe("false");
    expect(footer(box).hasAttribute("data-more")).toBe(false);
    await userEvent.click(more);
    expect(more.getAttribute("aria-expanded")).toBe("true");
    expect(footer(box).hasAttribute("data-more")).toBe(true);
    await userEvent.click(more);
    expect(footer(box).hasAttribute("data-more")).toBe(false);
  });

  it("offers nothing to open when there is nothing behind it", () => {
    card({ allowsSession: false, allowsPersist: false });
    expect(screen.queryByRole("button", { name: "更多选项…" })).toBeNull();
  });

  it("keeps allow and refuse as the two buttons of the first row, whatever else is open", async () => {
    const box = card();
    const row = [...footer(box).querySelectorAll<HTMLElement>(".btn[data-primary], .btn[data-deny]")];
    expect(row.map((b) => b.textContent)).toEqual(expect.arrayContaining(["允许这一次", "拒绝"]));
    await userEvent.click(screen.getByRole("button", { name: "更多选项…" }));
    expect(footer(box).querySelectorAll(".btn[data-primary], .btn[data-deny]")).toHaveLength(2);
  });
});

describe("the same card on a wider screen", () => {
  it("draws every choice as a button and no 'more options' to open", () => {
    delete document.documentElement.dataset.fold;
    card();
    expect(screen.queryByRole("button", { name: "更多选项…" })).toBeNull();
    expect(screen.getByRole("button", { name: "允许这一次" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "拒绝" })).toBeTruthy();
  });
});
