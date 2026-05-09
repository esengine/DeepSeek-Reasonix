import { describe, expect, it } from "vitest";
import { buildFeedbackDiagnostic, buildFeedbackIssueUrl } from "../src/cli/ui/feedback.js";

const FIXTURE = {
  version: "0.34.1",
  latestVersion: "0.34.1",
  platform: "win32",
  osRelease: "10.0.26200",
  termProgram: "Windows Terminal",
  term: "xterm-256color",
  colorTerm: "truecolor",
  inWindowsTerminal: true,
  inTmux: false,
  inSsh: false,
  wslDistro: undefined as string | undefined,
  cols: 142,
  rows: 40,
  nodeVersion: "v22.10.0",
  locale: "zh-CN",
  theme: "tokyo-night",
  model: "deepseek-v4-flash",
  reasoningEffort: "high",
  editMode: "auto",
  planMode: false,
  mcpServerCount: 3,
  sessionId: "code-reasonix",
};

describe("buildFeedbackDiagnostic", () => {
  it("emits all flicker-relevant fields when supplied", () => {
    const out = buildFeedbackDiagnostic(FIXTURE);
    expect(out).toContain("**Reasonix**: 0.34.1 (latest)");
    expect(out).toContain("**Platform**: win32 (10.0.26200)");
    expect(out).toContain(
      "**Terminal**: Windows Terminal (TERM_PROGRAM=Windows Terminal, TERM=xterm-256color, COLORTERM=truecolor, WT_SESSION=set)",
    );
    expect(out).toContain("**Size**: 142×40");
    expect(out).toContain("**Node**: v22.10.0");
    expect(out).toContain("**Locale**: zh-CN");
    expect(out).toContain("**Theme**: tokyo-night");
    expect(out).toContain("**Model**: deepseek-v4-flash · effort=high");
    expect(out).toContain("**Mode**: edit=auto · plan=off");
    expect(out).toContain("**MCP**: 3 server(s)");
    expect(out).toContain("**Session**: code-reasonix");
    expect(out).toContain("<!-- describe what you were doing when this happened -->");
  });

  it("only emits fields the codebase advertises — no surprise leaks", () => {
    const out = buildFeedbackDiagnostic(FIXTURE);
    const fieldNames = out
      .split("\n")
      .filter((l) => l.startsWith("**"))
      .map((l) => l.split(":")[0]);
    expect(fieldNames).toEqual([
      "**Reasonix**",
      "**Platform**",
      "**Terminal**",
      "**Size**",
      "**Node**",
      "**Locale**",
      "**Theme**",
      "**Model**",
      "**Mode**",
      "**MCP**",
      "**Session**",
    ]);
  });

  it("renders the latest-version comparison when behind", () => {
    const out = buildFeedbackDiagnostic({ ...FIXTURE, latestVersion: "0.35.0" });
    expect(out).toContain("**Reasonix**: 0.34.1 (latest: 0.35.0)");
  });

  it("omits optional fields cleanly when absent", () => {
    const out = buildFeedbackDiagnostic({
      version: "0.34.1",
      platform: "linux",
      osRelease: "6.6.0",
      nodeVersion: "v22.10.0",
      locale: "EN",
      model: "deepseek-v4-flash",
    });
    expect(out).toContain("**Reasonix**: 0.34.1");
    expect(out).not.toContain("(latest");
    expect(out).not.toContain("**Size**");
    expect(out).not.toContain("**Theme**");
    expect(out).not.toContain("**MCP**");
    expect(out).not.toContain("**Session**");
    expect(out).toContain("**Mode**: plan=off");
  });

  it("flags WSL / tmux / ssh when those env markers are set", () => {
    const out = buildFeedbackDiagnostic({
      ...FIXTURE,
      inWindowsTerminal: false,
      inTmux: true,
      inSsh: true,
      wslDistro: "Ubuntu-22.04",
    });
    expect(out).toContain("TMUX=set");
    expect(out).toContain("SSH_TTY=set");
    expect(out).toContain("WSL=Ubuntu-22.04");
    expect(out).not.toContain("WT_SESSION=set");
  });

  it("does not include API keys, file paths, or transcript content", () => {
    const out = buildFeedbackDiagnostic(FIXTURE);
    expect(out).not.toMatch(/sk-[a-zA-Z0-9]/);
    expect(out).not.toMatch(/[a-zA-Z]:\\|\/home\/|\/Users\//);
    expect(out).not.toMatch(/<\|user\|>|<\|assistant\|>|tool_call/);
  });
});

describe("buildFeedbackIssueUrl", () => {
  it("encodes the diagnostic into the body query param so the issue page opens pre-filled", () => {
    const diagnostic = buildFeedbackDiagnostic(FIXTURE);
    const url = buildFeedbackIssueUrl(diagnostic);
    expect(url.startsWith("https://github.com/esengine/DeepSeek-Reasonix/issues/new?body=")).toBe(
      true,
    );
    const decoded = decodeURIComponent(url.split("?body=")[1] ?? "");
    expect(decoded).toBe(diagnostic);
  });

  it("caps body length so a runaway diagnostic can't blow past GitHub's URL limit", () => {
    const huge = `${"x".repeat(20000)}`;
    const url = buildFeedbackIssueUrl(huge);
    expect(url.length).toBeLessThan(20000);
    expect(url).toMatch(/^https:\/\/github\.com\/esengine\/DeepSeek-Reasonix\/issues\/new\?body=/);
  });
});
