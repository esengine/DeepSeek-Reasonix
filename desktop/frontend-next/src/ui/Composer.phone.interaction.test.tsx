// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Composer } from "./Composer";
import { MockPort } from "../port/mock";
import { boot, STORAGE } from "../i18n";

const PHONE = "rail side scene phone";

beforeEach(() => {
  localStorage.clear();
  localStorage.setItem(STORAGE, "zh");
  boot();
});
afterEach(() => {
  cleanup();
  localStorage.clear();
  delete document.documentElement.dataset.fold;
  vi.restoreAllMocks();
});

function draw(running = false) {
  return render(
    <Composer port={new MockPort()} status={null} running={running} queued={false} onSubmit={vi.fn(async () => true)} onChanged={vi.fn()} onError={vi.fn()} />,
  ).container;
}

const tools = (box: HTMLElement) => box.querySelector<HTMLElement>(".turntools")!;

// A phone's tool row is add, model, send; the rest of the row's controls are
// one tap behind a single button rather than gone.
describe("the composer's tool row on a phone", () => {
  it("offers one button for the controls that are not add, model or send", async () => {
    document.documentElement.dataset.fold = PHONE;
    const box = draw();
    const more = screen.getByRole("button", { name: "更多工具" });
    expect(more.getAttribute("aria-expanded")).toBe("false");
    expect(tools(box).hasAttribute("data-more")).toBe(false);
    await userEvent.click(more);
    expect(more.getAttribute("aria-expanded")).toBe("true");
    expect(tools(box).hasAttribute("data-more")).toBe(true);
    await userEvent.click(more);
    expect(tools(box).hasAttribute("data-more")).toBe(false);
  });

  it("puts that button in the tool row, after the model", () => {
    document.documentElement.dataset.fold = PHONE;
    const box = draw();
    const kids = [...tools(box).children];
    const model = kids.findIndex((el) => el.classList.contains("studio-model-group"));
    const more = kids.findIndex((el) => el.classList.contains("turn-more"));
    expect(model).toBeGreaterThanOrEqual(0);
    expect(more).toBeGreaterThan(model);
  });

  it("leaves a wider screen's row exactly as it was", () => {
    const box = draw();
    expect(screen.queryByRole("button", { name: "更多工具" })).toBeNull();
    expect(tools(box).querySelector(".turn-more")).toBeNull();
  });
});

describe("the stop button", () => {
  it("carries the board's pause mark as well as the square, and the stylesheet picks one", () => {
    const box = draw(true);
    const stop = box.querySelector(".btn.stop")!;
    expect(stop.querySelector(".ic-stop")).not.toBeNull();
    expect(stop.querySelector(".ic-pause")).not.toBeNull();
  });
});
