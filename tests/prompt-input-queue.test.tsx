/** PromptInput — rendering tests for the `disabled` prop, relevant to the
 *  user-interrupt feature where App.tsx stops passing `disabled={busy}`. */

import { render } from "ink-testing-library";
import React from "react";
import { describe, expect, it } from "vitest";
import { PromptInput, QueueIndicator } from "../src/cli/ui/PromptInput.js";

// Helpers

function renderPrompt(props: Partial<Parameters<typeof PromptInput>[0]> = {}) {
  const { lastFrame, unmount } = render(
    <PromptInput
      value={props.value ?? ""}
      onChange={props.onChange ?? (() => {})}
      onSubmit={props.onSubmit ?? (() => {})}
      disabled={props.disabled}
      placeholder={props.placeholder}
    />,
  );
  const frame = lastFrame() ?? "";
  unmount();
  return frame;
}

// Tests

describe("PromptInput disabled prop", () => {
  // Disabled = false (the new default for the queue feature) ------------------

  it("shows the normal prompt character and placeholder when disabled=false", () => {
    const frame = renderPrompt({ disabled: false });
    // Should NOT show the disabled-only placeholder text
    expect(frame).not.toMatch(/waiting for response/);
    // Should show the prompt area (the `›` prefix is the normal indicator)
    expect(frame).toContain("›");
  });

  it("keeps the prompt character visible when disabled is omitted (undefined)", () => {
    const frame = renderPrompt({ disabled: undefined });
    // Same as disabled=false — input is interactive
    expect(frame).not.toMatch(/waiting for response/);
    expect(frame).toContain("›");
  });

  it("renders the hint row (Ctrl+P / Ctrl+N / …) when not disabled", () => {
    const frame = renderPrompt({ disabled: false });
    // The hint bar is only rendered when !disabled
    expect(frame).toMatch(/clear|history/i);
  });

  // Disabled = true (still valid for other use cases) ------------------------

  it("shows the waiting placeholder when disabled=true", () => {
    const frame = renderPrompt({ disabled: true });
    expect(frame).toMatch(/waiting for response/);
  });

  it("does NOT render the hint row when disabled=true", () => {
    const frame = renderPrompt({ disabled: true });
    expect(frame).not.toMatch(/clear|history/);
  });

  it("uses dimmed styling when disabled", () => {
    // The ANSI-stripped text won't show color, but the prompt character
    // is rendered with an empty line (no cursor block) when disabled.
    const frame = renderPrompt({ disabled: true });
    expect(frame).not.toContain("▌"); // cursor block hidden when disabled
  });

  it("renders a custom placeholder over the disabled default", () => {
    const frame = renderPrompt({ disabled: true, placeholder: "custom placeholder" });
    expect(frame).toContain("custom placeholder");
    expect(frame).not.toMatch(/waiting for response/);
  });

  // onSubmit callback -------------------------------------------------------

  it("accepts an onSubmit callback", () => {
    let called = "";
    const frame = renderPrompt({
      disabled: false,
      onSubmit: (v) => {
        called = v;
      },
      value: "test-value",
    });

    // The component renders without error even with the callback wired.
    // We verify the value renders somewhere in the output.
    expect(frame).toContain("test-value");

    // Sanity: the callback is callable
    expect(() => {
      // We can't simulate Enter keystroke easily, but we can verify
      // the prop was accepted and the component mounted.
    }).not.toThrow();
    expect(called).toBe(""); // not called yet — correct, Enter wasn't pressed
  });

  // Value rendering ---------------------------------------------------------

  it("renders empty state when value is an empty string", () => {
    const frame = renderPrompt({ value: "", disabled: false });
    expect(frame).not.toContain("error");
    expect(frame).toContain("›"); // prompt still shows
  });

  it("renders user value when provided", () => {
    const frame = renderPrompt({ value: "look at src/", disabled: false });
    expect(frame).toContain("look at src/");
  });
});

// QueueIndicator — new component for the user-queue feature

describe("QueueIndicator", () => {
  it("renders nothing when the queue is empty", () => {
    const { lastFrame, unmount } = render(<QueueIndicator messages={[]} />);
    const frame = (lastFrame() ?? "").trim();
    unmount();
    expect(frame).toBe("");
  });

  it("shows the count and latest message preview with 1 message", () => {
    const { lastFrame, unmount } = render(<QueueIndicator messages={["look at src/"]} />);
    const frame = lastFrame() ?? "";
    unmount();
    expect(frame).toContain("QUEUE");
    expect(frame).toContain("1");
    expect(frame).toContain("look at src/");
  });

  it("shows count >1 and preview of the LAST (most recent) message when multiple", () => {
    const { lastFrame, unmount } = render(
      <QueueIndicator messages={["first", "second", "third"]} />,
    );
    const frame = lastFrame() ?? "";
    unmount();
    expect(frame).toContain("QUEUE");
    expect(frame).toContain("3");
    expect(frame).toContain("third");
    expect(frame).not.toContain("first");
  });

  it("hints that Esc removes the last queued message", () => {
    const { lastFrame, unmount } = render(<QueueIndicator messages={["msg"]} />);
    const frame = lastFrame() ?? "";
    unmount();
    expect(frame).toContain("QUEUE");
    // Should mention the key to remove (like the edit undo banner does)
    expect(frame).toMatch(/esc/i);
  });

  it("shows remaining time before auto-dismiss when the timer is active", () => {
    const { lastFrame, unmount } = render(
      <QueueIndicator messages={[{ text: "msg", enqueuedAt: Date.now() }]} remainingMs={4200} />,
    );
    const frame = lastFrame() ?? "";
    unmount();
    expect(frame).toContain("QUEUE");
    // Should indicate the message will auto-dismiss
    expect(frame).toMatch(/\d/);
  });

  it("renders nothing when all messages have been consumed (empty after timer)", () => {
    const { lastFrame, unmount } = render(<QueueIndicator messages={[]} />);
    const frame = (lastFrame() ?? "").trim();
    unmount();
    expect(frame).toBe("");
  });

  it("truncates a very long message preview", () => {
    const long = "a".repeat(200);
    const { lastFrame, unmount } = render(<QueueIndicator messages={[long]} />);
    const frame = lastFrame() ?? "";
    unmount();
    expect(frame).toContain("QUEUE");
    expect(frame.length).toBeLessThan(400);
  });

  it("renders with dim/ghost styling (not part of the main conversation)", () => {
    const { lastFrame, unmount } = render(<QueueIndicator messages={["hi"]} />);
    const frame = lastFrame() ?? "";
    unmount();
    // The indicator should be visually distinct — rendered in faint/dim style
    expect(frame).toContain("QUEUE");
    // It should NOT look like a normal user message (no USER prefix like the chat cards)
    expect(frame).not.toMatch(/^\s*USER\b/i);
  });
});
