import { render } from "ink-testing-library";
import React from "react";
import { describe, expect, it } from "vitest";
import { SlashSuggestions } from "../src/cli/ui/SlashSuggestions.js";
import {
  SLASH_COMMANDS,
  SLASH_GROUP_ORDER,
  type SlashCommandSpec,
  countAdvancedCommands,
  suggestSlashCommands,
} from "../src/cli/ui/slash.js";

function makeCommands(count: number): SlashCommandSpec[] {
  const groups = ["chat", "setup", "info", "session", "extend", "code", "jobs"] as const;
  return Array.from({ length: count }, (_, i) => ({
    cmd: `cmd${i.toString().padStart(2, "0")}`,
    summary: `summary ${i}`,
    group: groups[Math.floor(i / 5) % groups.length],
  }));
}

function suggestionElement(
  matches: SlashCommandSpec[],
  selectedIndex: number,
  advancedHidden = 0,
): React.ReactElement {
  return React.createElement(SlashSuggestions, {
    matches,
    selectedIndex,
    groupMode: true,
    advancedHidden,
  });
}

function renderSuggestions(selectedIndex: number): string {
  const { lastFrame, unmount } = render(
    suggestionElement(suggestSlashCommands("", true), selectedIndex, countAdvancedCommands(true)),
  );
  const frame = lastFrame() ?? "";
  unmount();
  return frame;
}

function visibleCommandOrder(
  frame: string,
  commands: readonly SlashCommandSpec[] = SLASH_COMMANDS,
): string[] {
  const names = new Set(commands.map((spec) => `/${spec.cmd}`));
  return frame
    .split(/\r?\n/)
    .map((line) => /^\s*(?:▸\s*)?(\/\w+)\b/.exec(line)?.[1] ?? "")
    .filter((token) => names.has(token));
}

function firstVisibleCommand(
  frame: string,
  commands: readonly SlashCommandSpec[] = SLASH_COMMANDS,
): string | undefined {
  return visibleCommandOrder(frame, commands)[0];
}

function hiddenAboveCount(frame: string): number {
  const match = /↑ (\d+) above/.exec(frame);
  return match ? Number(match[1]) : 0;
}

function visibleGroupOrder(frame: string): string[] {
  return frame
    .split(/\r?\n/)
    .map(
      (line) => /^\s*(CHAT|SETUP|INFO|SESSION|EXTEND|CODE|JOBS|ADVANCED)\b/.exec(line)?.[1] ?? "",
    )
    .filter(Boolean);
}

describe("SlashSuggestions", () => {
  it("renders visible bare-slash groups in the shared order", () => {
    const frame = renderSuggestions(0);
    const visibleGroups = visibleGroupOrder(frame);
    expect(visibleGroups).toEqual(
      SLASH_GROUP_ORDER.filter((group) => group !== "advanced")
        .map((group) => group.toUpperCase())
        .slice(0, visibleGroups.length),
    );
  });

  it("renders the bare slash release command surface as 41 total commands", () => {
    const matches = suggestSlashCommands("", true);
    const names = matches.map((spec) => spec.cmd);
    const { lastFrame, unmount } = render(
      suggestionElement(matches, 0, countAdvancedCommands(true)),
    );
    const frame = lastFrame() ?? "";
    unmount();

    expect(matches).toHaveLength(41);
    expect(names).toContain("language");
    expect(names).toContain("btw");
    expect(countAdvancedCommands(true)).toBe(11);
    expect(frame).toContain("41 commands");
    expect(frame).toContain("+ 11 advanced");
  });

  it("surfaces /language for typed language prefixes", () => {
    expect(suggestSlashCommands("lan").map((spec) => spec.cmd)).toContain("language");
  });

  it("keeps the command order stable while the selected row moves in grouped browse mode", () => {
    const first = visibleCommandOrder(renderSuggestions(0));
    const middle = visibleCommandOrder(renderSuggestions(10));
    const last = visibleCommandOrder(renderSuggestions(18));

    expect(first).toEqual(middle);
    expect(middle).toEqual(last);
    const matches = suggestSlashCommands("", true);
    expect(first).toEqual(matches.slice(0, first.length).map((spec) => `/${spec.cmd}`));
  });

  it("scrolls through every command in grouped browse mode when the list is taller than the window", () => {
    const commands = makeCommands(30);
    const { lastFrame, rerender, unmount } = render(suggestionElement(commands, 0));

    rerender(suggestionElement(commands, commands.length - 1));
    const frame = lastFrame() ?? "";
    unmount();

    expect(visibleCommandOrder(frame, commands)).toContain("/cmd29");
    expect(hiddenAboveCount(frame)).toBe(10);
  });

  it("only advances the grouped window when selection crosses a visible boundary", () => {
    const commands = makeCommands(30);
    const { lastFrame, rerender, unmount } = render(suggestionElement(commands, 0));
    const firstAtStart = firstVisibleCommand(lastFrame() ?? "", commands);

    for (let selected = 1; selected < 20; selected += 1) {
      rerender(suggestionElement(commands, selected));
      expect(firstVisibleCommand(lastFrame() ?? "", commands)).toBe(firstAtStart);
    }

    rerender(suggestionElement(commands, 20));
    expect(firstVisibleCommand(lastFrame() ?? "", commands)).toBe("/cmd01");

    rerender(suggestionElement(commands, 21));
    expect(firstVisibleCommand(lastFrame() ?? "", commands)).toBe("/cmd02");
    unmount();
  });

  it("renders each visible command as one row instead of wrapping selected text into extra blocks", () => {
    const frame = renderSuggestions(7);
    const visibleRows = frame.split(/\r?\n/).filter((line) => /^\s*(?:▸\s*)?\/\w+\b/.test(line));
    const visibleCommands = visibleCommandOrder(frame);

    expect(visibleRows).toHaveLength(visibleCommands.length);
    expect(visibleRows.some((line) => line.includes("show the full command reference"))).toBe(true);
  });

  it("keeps bottom-window command rows paired with their own descriptions", () => {
    const commands = makeCommands(30).map((spec, i) => ({
      ...spec,
      summary: `description-for-${spec.cmd}-unique-${i}`,
    }));
    const { lastFrame, rerender, unmount } = render(suggestionElement(commands, 0));

    rerender(suggestionElement(commands, commands.length - 1));
    const frame = lastFrame() ?? "";
    unmount();

    const commandRows = frame.split(/\r?\n/).filter((line) => /^\s*(?:▸\s*)?\/\w+\b/.test(line));
    expect(commandRows).toContainEqual(expect.stringContaining("/cmd29"));
    expect(commandRows.find((line) => line.includes("/cmd29"))).toContain(
      "description-for-cmd29-unique-29",
    );
    expect(commandRows.find((line) => line.includes("/cmd29"))).not.toContain(
      "description-for-cmd23-unique-23",
    );
  });

  it("counts group headers inside the fixed visible row budget", () => {
    const commands = makeCommands(30);
    const frame =
      render(
        React.createElement(SlashSuggestions, {
          matches: commands,
          selectedIndex: commands.length - 1,
          groupMode: true,
        }),
      ).lastFrame() ?? "";

    const visibleBodyRows = frame
      .split(/\r?\n/)
      .filter((line) =>
        /^(\s*(?:CHAT|SETUP|INFO|SESSION|EXTEND|CODE|JOBS)|\s*(?:▸\s*)?\/\w+\b)/.test(line),
      );
    expect(visibleBodyRows.length).toBeLessThanOrEqual(24);
  });

  it("survives matches null → non-empty → null transitions without a hook-order crash", () => {
    // Reproducer for the "Rendered more hooks than during the previous
    // render" crash: useEffect used to live AFTER the early returns, so
    // the hook count flipped between 3 and 4 across renders.
    const commands = makeCommands(5);
    const { rerender, unmount } = render(
      React.createElement(SlashSuggestions, {
        matches: commands,
        selectedIndex: 0,
        groupMode: true,
      }),
    );
    expect(() => {
      rerender(
        React.createElement(SlashSuggestions, {
          matches: null,
          selectedIndex: 0,
          groupMode: true,
        }),
      );
      rerender(
        React.createElement(SlashSuggestions, {
          matches: [],
          selectedIndex: 0,
          groupMode: true,
        }),
      );
      rerender(
        React.createElement(SlashSuggestions, {
          matches: commands,
          selectedIndex: 1,
          groupMode: true,
        }),
      );
    }).not.toThrow();
    unmount();
  });
});
