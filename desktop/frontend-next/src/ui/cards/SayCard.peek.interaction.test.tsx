// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { act, cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { SayCard } from "./SayCard";
import { Folding } from "../Folding";
import { FOLD_DEFAULTS, setFoldModes, setShowsThinkingPreview } from "../../state/prefs";

afterEach(() => {
  cleanup();
  setFoldModes(FOLD_DEFAULTS);
  setShowsThinkingPreview(false);
});

const item = { t: "say" as const, id: "s", text: "answer", reasoning: "先检查配置文件。然后再看日志。", done: true };
const peek = (el: HTMLElement) => el.querySelector(".thinking-preview");

describe("collapsed thinking previews", () => {
  it("shows the opening text only when enabled and collapsed", async () => {
    const { container } = render(<SayCard item={item} />);
    expect(peek(container)).toBeNull();
    act(() => setShowsThinkingPreview(true));
    expect(peek(container)?.textContent).toBe(item.reasoning);
    await userEvent.click(container.querySelector("summary")!);
    expect(peek(container)).toBeNull();
    await userEvent.click(container.querySelector("summary")!);
    expect(peek(container)?.textContent).toBe(item.reasoning);
    act(() => setShowsThinkingPreview(false));
    expect(peek(container)).toBeNull();
  });

  it("retains the opening as more reasoning streams, then survives completion", () => {
    setShowsThinkingPreview(true);
    const { container, rerender } = render(<SayCard item={{ ...item, done: false, text: "", reasoning: "  First step." }} />);
    expect(peek(container)?.textContent).toBe("First step.");
    const reasoning = `  First step.\n${"More reasoning. ".repeat(30)}`;
    rerender(<SayCard item={{ ...item, done: false, text: "", reasoning }} />);
    expect(peek(container)?.textContent).toBe(reasoning.trimStart());
    rerender(<SayCard item={{ ...item, reasoning }} />);
    expect(peek(container)?.textContent).toBe(reasoning.trimStart());
  });

  it("opens while thinking, then previews when the answer starts, unless manually overridden", async () => {
    setFoldModes({ thinking: "live" });
    setShowsThinkingPreview(true);
    const { container, rerender } = render(<SayCard item={{ ...item, done: false, text: "" }} />);
    const details = () => container.querySelector("details")!;
    expect(details().open).toBe(true);
    expect(peek(container)).toBeNull();
    rerender(<SayCard item={{ ...item, done: false }} />);
    expect(details().open).toBe(false);
    expect(peek(container)?.textContent).toBe(item.reasoning);
    await userEvent.click(container.querySelector("summary")!);
    rerender(<SayCard item={item} />);
    expect(details().open).toBe(true);
    expect(peek(container)).toBeNull();
    act(() => setFoldModes({ thinking: "folded" }));
    expect(details().open).toBe(true);
  });

  it("lets a manually collapsed block preview even when the default is expanded", async () => {
    setFoldModes({ thinking: "open" });
    setShowsThinkingPreview(true);
    const { container } = render(<SayCard item={item} />);
    expect(peek(container)).toBeNull();
    await userEvent.click(container.querySelector("summary")!);
    expect(peek(container)?.textContent).toBe(item.reasoning);
    act(() => setFoldModes({ thinking: "live" }));
    expect(peek(container)?.textContent).toBe(item.reasoning);
  });

  it("does not render a disclosure or preview for blank reasoning", () => {
    setShowsThinkingPreview(true);
    const { container } = render(<SayCard item={{ ...item, reasoning: " \n " }} />);
    expect(container.querySelector("details")).toBeNull();
    expect(peek(container)).toBeNull();
  });
});

describe("the independent preview setting", () => {
  it("changes the transcript and demonstration without presets changing the preview preference", async () => {
    const settings = render(<Folding />);
    const card = render(<SayCard item={item} />);
    const sw = screen.getByRole("switch");
    expect(sw.getAttribute("aria-checked")).toBe("false");
    await userEvent.click(sw);
    expect(peek(card.container)?.textContent).toBe(item.reasoning);
    expect(settings.container.querySelector('.fp-block[data-part="thinking"] .thinking-preview')).not.toBeNull();
    const choices = settings.container.querySelector('[role="radiogroup"][aria-label]')!;
    const presets = within(choices as HTMLElement).getAllByRole("radio");
    await userEvent.click(presets[3]);
    expect(sw.getAttribute("aria-checked")).toBe("true");
    expect(card.container.querySelector("details")!.open).toBe(true);
    expect(peek(card.container)).toBeNull();
    await userEvent.click(presets[1]);
    expect(sw.getAttribute("aria-checked")).toBe("true");
    expect(peek(card.container)?.textContent).toBe(item.reasoning);
    await userEvent.click(sw);
    expect(peek(card.container)).toBeNull();
    expect(settings.container.querySelector(".thinking-preview")).toBeNull();
  });
});
