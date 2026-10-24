import { describe, expect, it } from "vitest";
import { toMarkdown } from "./sessionExportMarkdown";

/** Lines exactly as the kernel writes them: a provider message per line. */
const lines = [
  `{"role":"system","content":"you are a coding agent"}`,
  `{"role":"user","content":"为什么构建失败？"}`,
  `{"role":"assistant","content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"shell","arguments":"{}"}}]}`,
  `{"role":"tool","content":"巨大的工具输出"}`,
  `{"role":"assistant","content":"因为缺少一个环境变量。"}`,
  `{"role":"user","content":"宿主注入的上下文","host_authored":true}`,
];

describe("a transcript written out as Markdown", () => {
  it("keeps the questions and the answers in order, from the kernel's own lines", () => {
    const md = toMarkdown("部署问题", lines.map((l) => JSON.parse(l)));
    expect(md).toBe("# 部署问题\n\n## 我\n\n为什么构建失败？\n\n## 助手\n\n因为缺少一个环境变量。\n");
  });

  it("leaves out tool results, system lines and host-authored turns", () => {
    const md = toMarkdown("t", lines.map((l) => JSON.parse(l)));
    expect(md).not.toContain("巨大的工具输出");
    expect(md).not.toContain("you are a coding agent");
    expect(md).not.toContain("宿主注入的上下文");
  });

  it("skips an assistant turn that only reached for a tool", () => {
    // The tool_calls turn above carries no content, so it writes no heading.
    const md = toMarkdown("t", [JSON.parse(lines[2])]);
    expect(md).toBe("# t\n");
  });

  it("writes a heading rather than an empty line for an untitled conversation", () => {
    const first = toMarkdown("", []).split("\n")[0];
    expect(first.startsWith("# ")).toBe(true);
    expect(first.length).toBeGreaterThan(2);
  });
});
