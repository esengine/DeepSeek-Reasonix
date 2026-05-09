import { render } from "ink-testing-library";
import React from "react";
import { describe, expect, it } from "vitest";
import { SlashSuggestions } from "../src/cli/ui/SlashSuggestions.js";
import {
  SLASH_COMMANDS,
  type SlashCommandSpec,
  countAdvancedCommands,
  orderSlashCommandsByGroup,
  suggestSlashCommands,
} from "../src/cli/ui/slash.js";

function makeCommands(count: number): SlashCommandSpec[] {
  const groups = ["setup", "info", "chat", "extend", "session", "code", "jobs"] as const;
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

function headerCount(frame: string, header: string): number {
  return frame.split(/\r?\n/).filter((line) => line.trim() === header).length;
}

function visibleBodyRows(frame: string): string[] {
  return frame
    .split(/\r?\n/)
    .map((line) => line.trim())
    .map((line) => {
      if (/^(SETUP|INFO|CHAT|EXTEND|SESSION|CODE|JOBS|ADVANCED|UNKNOWN)$/.test(line)) return line;
      return /^(?:▸\s*)?(\/\w+)\b/.exec(line)?.[1] ?? "";
    })
    .filter(Boolean);
}

function rowIndex(rows: readonly string[], token: string): number {
  const index = rows.findIndex((line) => line.startsWith(token));
  expect(index).toBeGreaterThanOrEqual(0);
  return index;
}

function selectedCommand(frame: string): string | undefined {
  return frame
    .split(/\r?\n/)
    .map((line) => /^\s*▸\s*(\/\w+)\b/.exec(line)?.[1])
    .find((token): token is string => Boolean(token));
}

describe("SlashSuggestions", () => {
  it("renders the bare slash release command surface as 37 total commands", () => {
    const matches = suggestSlashCommands("", true);
    const names = matches.map((spec) => spec.cmd);
    const { lastFrame, unmount } = render(
      suggestionElement(matches, 0, countAdvancedCommands(true)),
    );
    const frame = lastFrame() ?? "";
    unmount();

    expect(matches).toHaveLength(37);
    expect(names).toContain("language");
    expect(countAdvancedCommands(true)).toBe(12);
    expect(frame).toContain("37 commands");
    expect(frame).toContain("+ 12 advanced");
  });

  it("surfaces /language for typed language prefixes", () => {
    expect(suggestSlashCommands("lan").map((spec) => spec.cmd)).toContain("language");
  });

  it("groups bare slash suggestions under one header per category", () => {
    const frame = renderSuggestions(0);
    const rows = visibleBodyRows(frame);
    const setup = rowIndex(rows, "SETUP");
    const language = rowIndex(rows, "/language");
    const preset = rowIndex(rows, "/preset");
    const model = rowIndex(rows, "/model");
    const info = rowIndex(rows, "INFO");
    const status = rowIndex(rows, "/status");

    expect(headerCount(frame, "SETUP")).toBe(1);
    expect(headerCount(frame, "INFO")).toBe(1);
    expect(rows.filter((row) => row === "SETUP" || row === "INFO")).toEqual(["SETUP", "INFO"]);
    expect(rows.slice(setup, info)).toEqual(["SETUP", "/language", "/preset", "/model"]);
    expect([language, preset, model]).toEqual([setup + 1, setup + 2, setup + 3]);
    expect(status).toBe(info + 1);
  });

  it("maps selectedIndex to grouped command rows without counting group headers", () => {
    expect(selectedCommand(renderSuggestions(0))).toBe("/language");
    expect(selectedCommand(renderSuggestions(1))).toBe("/preset");
    expect(selectedCommand(renderSuggestions(2))).toBe("/model");
    expect(selectedCommand(renderSuggestions(3))).toBe("/status");

    const rows = visibleBodyRows(renderSuggestions(1));
    expect(rows.slice(0, 5)).toEqual(["SETUP", "/language", "/preset", "/model", "INFO"]);
    expect(selectedCommand(renderSuggestions(1))).not.toBe("SETUP");
    expect(selectedCommand(renderSuggestions(3))).not.toBe("INFO");
  });

  it("moves from the first grouped command to the second grouped command on the next index", () => {
    const { lastFrame, rerender, unmount } = render(
      suggestionElement(suggestSlashCommands("", true), 0, countAdvancedCommands(true)),
    );
    expect(selectedCommand(lastFrame() ?? "")).toBe("/language");

    rerender(suggestionElement(suggestSlashCommands("", true), 1, countAdvancedCommands(true)));
    expect(selectedCommand(lastFrame() ?? "")).toBe("/preset");
    unmount();
  });

  it("keeps filtered slash suggestions grouped even with usage counts", () => {
    const matches = suggestSlashCommands("c", true, { compact: 100, cost: 90, checkpoint: 80 });
    const { lastFrame, unmount } = render(
      React.createElement(SlashSuggestions, {
        matches,
        selectedIndex: 0,
        groupMode: true,
      }),
    );
    const frame = lastFrame() ?? "";
    const rows = visibleBodyRows(frame);
    unmount();

    expect(headerCount(frame, "INFO")).toBe(1);
    expect(headerCount(frame, "CHAT")).toBe(1);
    expect(headerCount(frame, "CODE")).toBe(1);
    expect(rows).toEqual([
      "INFO",
      "/cost",
      "/context",
      "CHAT",
      "/compact",
      "/new",
      "CODE",
      "/checkpoint",
      "/commit",
      "/cwd",
    ]);
  });

  it("keeps the grouped command order stable while the selected row moves in grouped browse mode", () => {
    const first = visibleCommandOrder(renderSuggestions(0));
    const middle = visibleCommandOrder(renderSuggestions(10));
    const last = visibleCommandOrder(renderSuggestions(18));

    expect(first).toEqual(middle);
    expect(middle).toEqual(last);
    const groupedMatches = orderSlashCommandsByGroup(suggestSlashCommands("", true));
    expect(first).toEqual(groupedMatches.slice(0, first.length).map((spec) => `/${spec.cmd}`));
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
        /^(\s*(?:SETUP|INFO|CHAT|EXTEND|SESSION|CODE|JOBS|UNKNOWN)|\s*(?:▸\s*)?\/\w+\b)/.test(line),
      );
    expect(visibleBodyRows.length).toBeLessThanOrEqual(24);
  });
});
