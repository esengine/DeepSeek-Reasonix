import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { Text } from "ink";
import React, { useState } from "react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { useCompletionPickers } from "../src/cli/ui/useCompletionPickers.js";
import { render } from "./helpers/ink-test.js";

const script = `export const meta = { name: "repo_audit", description: "Repo audit" }
return true
`;

function Harness({ initialInput, rootDir }: { initialInput: string; rootDir: string }) {
  const [input, setInput] = useState(initialInput);
  const pickers = useCompletionPickers({
    input,
    setInput,
    codeMode: { rootDir },
    rootDir,
    models: [],
    mcpServers: [],
    effortChoices: ["low", "medium", "high", "max"],
  });

  const matches = pickers.slashArgMatches;
  if (!matches || matches.length === 0) return <Text>empty</Text>;
  return <Text>{matches.join("\n")}</Text>;
}

describe("workflow arg completion", () => {
  let root: string;

  beforeEach(() => {
    root = mkdtempSync(join(tmpdir(), "reasonix-workflow-completion-"));
    mkdirSync(join(root, ".reasonix", "workflows"), { recursive: true });
    writeFileSync(join(root, ".reasonix", "workflows", "repo-audit.js"), script, "utf8");
  });

  afterEach(() => {
    rmSync(root, { recursive: true, force: true });
  });

  it("suggests saved workflow names after /workflows run", () => {
    const { lastFrame, unmount } = render(
      <Harness initialInput="/workflows run repo" rootDir={root} />,
      { stdout: process.stdout as never },
    );
    const frame = lastFrame() ?? "";

    expect(frame).toContain("repo_audit");
    unmount();
  });
});
