import { Text } from "ink";
import React, { useState } from "react";
import { describe, expect, it } from "vitest";
import { useCompletionPickers } from "../src/cli/ui/useCompletionPickers.js";
import { render } from "./helpers/ink-test.js";

function Harness({ initialInput }: { initialInput: string }) {
  const [input, setInput] = useState(initialInput);
  const pickers = useCompletionPickers({
    input,
    setInput,
    codeMode: { rootDir: process.cwd() },
    rootDir: process.cwd(),
    models: ["deepseek-v4-flash", "deepseek-v4-pro"],
    mcpServers: [],
    effortChoices: ["low", "medium", "high", "max"],
  });

  const matches = pickers.slashArgMatches;
  if (!matches || matches.length === 0) return <Text>empty</Text>;
  return <Text>{matches.join("\n")}</Text>;
}

describe("model arg completion", () => {
  it("suggests workflow-policy for /model", () => {
    const { lastFrame, unmount } = render(<Harness initialInput="/model work" />, {
      stdout: process.stdout as never,
    });
    const frame = lastFrame() ?? "";
    expect(frame).toContain("workflow-policy");
    unmount();
  });

  it("suggests policy values after /model workflow-policy", () => {
    const { lastFrame, unmount } = render(<Harness initialInput="/model workflow-policy " />, {
      stdout: process.stdout as never,
    });
    const frame = lastFrame() ?? "";
    expect(frame).toContain("inherit");
    expect(frame).toContain("flash");
    expect(frame).toContain("pro");
    expect(frame).toContain("mixed");
    expect(frame).toContain("auto");
    unmount();
  });
});
