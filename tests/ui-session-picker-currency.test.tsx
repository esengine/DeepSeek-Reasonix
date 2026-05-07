import { render } from "ink";
import React from "react";
import { describe, expect, it } from "vitest";
import { SessionPicker } from "../src/cli/ui/SessionPicker.js";
import type { SessionInfo } from "../src/memory/session.js";

function makeFakeStdout() {
  const chunks: string[] = [];
  return {
    columns: 120,
    rows: 30,
    isTTY: true,
    write(chunk: string) {
      chunks.push(chunk);
      return true;
    },
    on() {},
    off() {},
    text(): string {
      // biome-ignore lint/suspicious/noControlCharactersInRegex: stripping ANSI SGR codes
      return chunks.join("").replace(/\x1b\[[0-9;]*m/g, "");
    },
  };
}

function makeSession(currencyHint?: string): SessionInfo {
  return {
    name: "demo",
    path: "/tmp/demo.jsonl",
    size: 100,
    messageCount: 4,
    mtime: new Date("2026-05-06T00:00:00Z"),
    meta: {
      branch: "main",
      summary: "demo session",
      totalCostUsd: 0.05,
      turnCount: 2,
      workspace: "/repo",
      ...(currencyHint ? { balanceCurrency: currencyHint } : {}),
    },
  };
}

function renderPicker(sessions: SessionInfo[], walletCurrency: string | undefined): string {
  const stdout = makeFakeStdout();
  const { unmount } = render(
    React.createElement(SessionPicker, {
      sessions,
      workspace: "/repo",
      walletCurrency,
      onChoose: () => {},
    }),
    { stdout: stdout as any, stdin: process.stdin as any },
  );
  unmount();
  return stdout.text();
}

describe("SessionPicker — cost column follows wallet currency, not hardcoded ¥", () => {
  it("USD wallet prop: $0.05 (not ¥)", () => {
    const text = renderPicker([makeSession()], "USD");
    expect(text).toContain("$0.05");
    expect(text).not.toContain("¥");
  });

  it("CNY wallet prop: ¥0.36 (USD * 7.2)", () => {
    const text = renderPicker([makeSession()], "CNY");
    expect(text).toContain("¥0.36");
    expect(text).not.toContain("$0.05");
  });

  it("no wallet prop: per-row meta.balanceCurrency wins", () => {
    const text = renderPicker([makeSession("USD")], undefined);
    expect(text).toContain("$0.05");
    expect(text).not.toContain("¥");
  });

  it("neither prop nor meta: falls back to ¥ (unchanged from pre-fix)", () => {
    const text = renderPicker([makeSession()], undefined);
    expect(text).toContain("¥0.36");
    expect(text).not.toContain("$0.05");
  });
});
