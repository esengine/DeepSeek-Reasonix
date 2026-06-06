import { render } from "ink";
import React from "react";
import { afterEach, describe, expect, it } from "vitest";
import { ThemePicker, type ThemePickerOutcome } from "../src/cli/ui/ThemePicker.js";
import {
  type KeystrokeHandler,
  KeystrokeProvider,
  type KeystrokeReader,
  makeKeyEvent,
} from "../src/cli/ui/keystroke-context.js";
import type { KeyEvent } from "../src/cli/ui/stdin-reader.js";
import type { ThemeChoice } from "../src/cli/ui/theme/labels.js";
import { type ThemeName, listThemeNames } from "../src/cli/ui/theme/tokens.js";
import { setLanguageRuntime } from "../src/i18n/index.js";
import { makeFakeStdin, makeFakeStdout } from "./helpers/ink-stdio.js";

class FakeReader implements KeystrokeReader {
  private readonly handlers = new Set<KeystrokeHandler>();

  start(): void {
    // no-op
  }

  subscribe(handler: KeystrokeHandler): () => void {
    this.handlers.add(handler);
    return () => {
      this.handlers.delete(handler);
    };
  }

  feed(ev: Partial<KeyEvent>): void {
    const event = makeKeyEvent(ev);
    for (const handler of [...this.handlers]) handler(event);
  }
}

function renderPicker(props: {
  currentPreference: ThemeChoice;
  activeTheme: ThemeName;
}): string {
  const { stdout, unmount } = mountPicker(new FakeReader(), props, () => {});
  unmount();
  return stdout.text();
}

function mountPicker(
  reader: FakeReader,
  props: {
    currentPreference: ThemeChoice;
    activeTheme: ThemeName;
  },
  onChoose: (outcome: ThemePickerOutcome) => void,
) {
  const stdout = makeFakeStdout();
  const { unmount } = render(
    React.createElement(
      KeystrokeProvider,
      { reader },
      React.createElement(ThemePicker, {
        currentPreference: props.currentPreference,
        activeTheme: props.activeTheme,
        onChoose,
      }),
    ),
    { stdout: stdout as never, stdin: makeFakeStdin() as never },
  );
  return { stdout, unmount };
}

async function flush(): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, 0));
}

describe("ThemePicker", () => {
  afterEach(() => setLanguageRuntime("EN"));

  it("lists auto and all registered themes", () => {
    const text = renderPicker({ currentPreference: "auto", activeTheme: "graphite" });
    expect(text).toContain("auto");
    for (const name of listThemeNames()) {
      expect(text).toContain(name);
    }
  });

  it("marks the current preference and active theme", () => {
    const text = renderPicker({ currentPreference: "auto", activeTheme: "graphite" });
    expect(text).toMatch(/auto[\s\S]*current preference/);
    expect(text).toMatch(/graphite[\s\S]*active now/);
  });

  it("localizes labels while keeping the same theme ids", () => {
    setLanguageRuntime("zh-CN");
    const text = renderPicker({ currentPreference: "aurora", activeTheme: "aurora" });
    expect(text).toContain("极光 (aurora)");
    for (const name of listThemeNames()) {
      expect(text).toContain(`(${name})`);
    }
  });

  it("submits the resolved visible theme for a legacy current preference", async () => {
    const reader = new FakeReader();
    const outcomes: ThemePickerOutcome[] = [];
    const { unmount } = mountPicker(
      reader,
      { currentPreference: "dark", activeTheme: "graphite" },
      (outcome) => outcomes.push(outcome),
    );

    await flush();
    reader.feed({ return: true });

    expect(outcomes).toEqual([{ kind: "select", value: "graphite" }]);
    unmount();
  });

  it("renders the keybind hint footer", () => {
    const text = renderPicker({ currentPreference: "midnight", activeTheme: "midnight" });
    expect(text).toContain("↑↓");
    expect(text).toContain("⏎");
    expect(text).toContain("esc");
  });
});
