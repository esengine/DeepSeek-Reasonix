import React from "react";
import { describe, expect, it } from "vitest";
import { PlanCheckpointConfirm } from "../src/cli/ui/PlanCheckpointConfirm.js";
import { render } from "./helpers/ink-test.js";

describe("PlanCheckpointConfirm", () => {
  it("labels the primary action as finish when the last step is complete", () => {
    const { lastFrame, unmount } = render(
      <PlanCheckpointConfirm
        stepId="step-3"
        title="Run tests"
        completed={3}
        total={3}
        steps={[
          { id: "step-1", title: "Create file", action: "Create file" },
          { id: "step-2", title: "Update import", action: "Update import" },
          { id: "step-3", title: "Run tests", action: "Run tests" },
        ]}
        completedStepIds={new Set(["step-1", "step-2", "step-3"])}
        onChoose={() => {}}
      />,
    );

    const out = lastFrame() ?? "";
    expect(out).toContain("Finish");
    expect(out).not.toContain("Continue — run the next step");
    unmount();
  });
});
