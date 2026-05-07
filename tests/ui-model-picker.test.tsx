import { render } from "ink";
import React from "react";
import { describe, expect, it } from "vitest";
import { ModelPicker } from "../src/cli/ui/ModelPicker.js";
import { makeFakeStdin, makeFakeStdout } from "./helpers/ink-stdio.js";

function renderPicker(props: {
  models: ReadonlyArray<string> | null;
  current: string;
}): string {
  const stdout = makeFakeStdout();
  const { unmount } = render(
    React.createElement(ModelPicker, {
      models: props.models,
      current: props.current,
      onChoose: () => {},
    }),
    { stdout: stdout as never, stdin: makeFakeStdin() as never },
  );
  unmount();
  return stdout.text();
}

describe("ModelPicker (#371)", () => {
  it("lists API models when the catalog has loaded", () => {
    const text = renderPicker({
      models: ["deepseek-v4-flash", "deepseek-v4-pro", "deepseek-reasoner"],
      current: "deepseek-v4-flash",
    });
    expect(text).toContain("deepseek-v4-flash");
    expect(text).toContain("deepseek-v4-pro");
    expect(text).toContain("deepseek-reasoner");
  });

  it("marks the current model with a `current` tag", () => {
    const text = renderPicker({
      models: ["deepseek-v4-flash", "deepseek-v4-pro"],
      current: "deepseek-v4-pro",
    });
    expect(text).toMatch(/deepseek-v4-pro[\s\S]*current/);
  });

  it("shows loading hint when catalog is null", () => {
    const text = renderPicker({ models: null, current: "deepseek-v4-flash" });
    expect(text).toContain("loading catalog");
  });

  it("falls back to the known DeepSeek ids when catalog is null so the picker isn't empty on first open", () => {
    const text = renderPicker({ models: null, current: "deepseek-v4-flash" });
    expect(text).toContain("deepseek-v4-flash");
    expect(text).toContain("deepseek-v4-pro");
  });

  it("shows the explicit empty hint when catalog loaded but is empty", () => {
    const text = renderPicker({ models: [], current: "deepseek-v4-flash" });
    expect(text).toContain("catalog empty");
  });

  it("includes the current id in the list even when API didn't return it (handles stale catalog)", () => {
    const text = renderPicker({
      models: ["deepseek-v4-flash"],
      current: "deepseek-experimental-x",
    });
    expect(text).toContain("deepseek-experimental-x");
  });

  it("renders the keybind hint footer", () => {
    const text = renderPicker({
      models: ["deepseek-v4-flash"],
      current: "deepseek-v4-flash",
    });
    expect(text).toContain("↑↓");
    expect(text).toContain("⏎");
    expect(text).toContain("esc");
  });
});
