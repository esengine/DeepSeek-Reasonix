import { describe, expect, it } from "vitest";
import { buildFeedbackDiagnostic } from "../src/cli/ui/feedback.js";

const FIXTURE = {
  version: "0.34.1",
  platform: "win32",
  osRelease: "10.0.26200",
  termProgram: "Windows Terminal",
  term: "xterm-256color",
  nodeVersion: "v22.10.0",
  locale: "zh-CN",
  model: "deepseek-v4-flash",
  sessionId: "code-reasonix",
};

describe("buildFeedbackDiagnostic", () => {
  it("includes the seven advertised fields and nothing else by default", () => {
    const out = buildFeedbackDiagnostic(FIXTURE);
    expect(out).toContain("**Reasonix**: 0.34.1");
    expect(out).toContain("**Platform**: win32 (10.0.26200)");
    expect(out).toContain(
      "**Terminal**: Windows Terminal (TERM_PROGRAM=Windows Terminal, TERM=xterm-256color)",
    );
    expect(out).toContain("**Node**: v22.10.0");
    expect(out).toContain("**Locale**: zh-CN");
    expect(out).toContain("**Model**: deepseek-v4-flash");
    expect(out).toContain("**Session**: code-reasonix");
    expect(out).toContain("<!-- describe what you were doing when this happened -->");

    const fieldLines = out.split("\n").filter((l) => l.startsWith("**"));
    expect(fieldLines.map((l) => l.split(":")[0])).toEqual([
      "**Reasonix**",
      "**Platform**",
      "**Terminal**",
      "**Node**",
      "**Locale**",
      "**Model**",
      "**Session**",
    ]);
  });

  it("omits the Session line when sessionId is absent", () => {
    const { sessionId: _drop, ...rest } = FIXTURE;
    const out = buildFeedbackDiagnostic(rest);
    expect(out).not.toContain("**Session**");
    expect(out).toContain("**Reasonix**");
    expect(out).toContain("**Model**");
  });

  it("does not leak environment variables outside TERM_PROGRAM/TERM", () => {
    const out = buildFeedbackDiagnostic({
      ...FIXTURE,
      termProgram: undefined,
      term: undefined,
    });
    expect(out).toContain("**Terminal**: (unknown)");
    expect(out).not.toMatch(/PATH=|HOME=|API_KEY|DEEPSEEK/);
  });

  it("does not include API keys, file paths, or transcript content", () => {
    const out = buildFeedbackDiagnostic(FIXTURE);
    expect(out).not.toMatch(/sk-[a-zA-Z0-9]/);
    expect(out).not.toMatch(/[a-zA-Z]:\\|\/home\/|\/Users\//);
    expect(out).not.toMatch(/<\|user\|>|<\|assistant\|>|tool_call/);
  });
});
