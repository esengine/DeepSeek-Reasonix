import { afterEach, describe, expect, it, vi } from "vitest";

const saved: { name: string; content: string }[] = [];
vi.mock("../port/host", () => ({
  host: () => ({ saveText: async (name: string, content: string) => (saved.push({ name, content }), "saved") }),
}));
vi.mock("../port/download", () => ({ download: vi.fn() }));

import { saveSessionFile } from "./sessionExport";
import type { TreeSession } from "../port/hub";

afterEach(() => { saved.length = 0; });

const session = { path: "/s/one.jsonl", title: "部署问题", name: "one" } as TreeSession;
const hubOf = (content: string) => ({ exportSession: async () => ({ name: "one", content }) });

describe("saving a session as Markdown", () => {
  it("writes the user and assistant turns of a native archive, which keeps its messages in records", async () => {
    const archive = JSON.stringify({
      format: "reasonix-native-session",
      version: 2,
      records: [
        { role: "system", content: "you are a coding agent" },
        { role: "user", content: "为什么构建失败？" },
        { role: "assistant", content: "因为缺少一个环境变量。" },
      ],
    });
    await saveSessionFile(hubOf(archive), session, "md");
    expect(saved).toHaveLength(1);
    expect(saved[0].name).toBe("部署问题.md");
    expect(saved[0].content).toContain("为什么构建失败？");
    expect(saved[0].content).toContain("因为缺少一个环境变量。");
    expect(saved[0].content).not.toContain("you are a coding agent");
  });

  it("writes the turns of a line-per-message export the same way", async () => {
    const lines = [
      JSON.stringify({ role: "user", content: "你好" }),
      JSON.stringify({ role: "assistant", content: "你好，有什么可以帮你？" }),
    ].join("\n");
    await saveSessionFile(hubOf(lines), session, "md");
    expect(saved[0].content).toContain("你好，有什么可以帮你？");
  });

  it("keeps the whole archive for the JSON export", async () => {
    const archive = JSON.stringify({ format: "reasonix-native-session", version: 2, records: [{ role: "user", content: "hi" }] });
    await saveSessionFile(hubOf(archive), session);
    expect(saved[0].name).toBe("部署问题.json");
    expect(JSON.parse(saved[0].content).records).toHaveLength(1);
  });
});
