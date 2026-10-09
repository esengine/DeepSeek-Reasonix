import { describe, expect, it } from "vitest";
import { sessionInfoText } from "./sessionInfo";

describe("session handoff information", () => {
  it("names the four paths another agent needs, with the context directory kept distinct from the task log", () => {
    expect(sessionInfoText(
      { name: "20261002-handoff", path: "/home/me/.reasonix/sessions/20261002-handoff.jsonl" },
      "/home/me/project",
    )).toBe([
      "会话 ID: 20261002-handoff",
      "会话上下文路径: /home/me/.reasonix/sessions",
      "任务路径: /home/me/project",
      "任务日志: /home/me/.reasonix/sessions/20261002-handoff.jsonl",
    ].join("\n"));
  });

  it("keeps a Windows context directory intact", () => {
    expect(sessionInfoText(
      { name: "20261002-win", path: String.raw`C:\Users\me\.reasonix\sessions\20261002-win.jsonl` },
      String.raw`C:\work\project`,
    )).toContain(String.raw`会话上下文路径: C:\Users\me\.reasonix\sessions`);
  });
});
