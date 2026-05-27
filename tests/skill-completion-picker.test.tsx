import { Text } from "ink";
import React, { useState } from "react";
import { describe, expect, it } from "vitest";
import { useCompletionPickers } from "../src/cli/ui/useCompletionPickers.js";
import { render } from "./helpers/ink-test.js";

function Harness({ rootDir, initialInput }: { rootDir: string; initialInput: string }) {
  const [input, setInput] = useState(initialInput);
  const pickers = useCompletionPickers({
    input,
    setInput,
    codeMode: { rootDir },
    rootDir,
    models: null,
    mcpServers: [],
  });

  const matches = pickers.slashArgMatches;
  if (!matches || matches.length === 0) return <Text>empty</Text>;
  return <Text>{matches.join("\n")}</Text>;
}

describe("skill arg completion", () => {
  it("includes builtin skills for /skill", () => {
    const { lastFrame, unmount } = render(
      <Harness rootDir={process.cwd()} initialInput="/skill q" />,
      { stdout: process.stdout as never },
    );
    const frame = lastFrame() ?? "";
    expect(frame).toContain("qq");
    unmount();
  });
});
