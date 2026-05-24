// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
// biome-ignore lint/style/useImportType: The TSX transform needs React in scope.
import React, { useRef, useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("@tauri-apps/plugin-dialog", () => ({
  open: vi.fn(),
}));

import { Composer } from "../dashboard/src/ui/composer";

afterEach(() => {
  cleanup();
});

function renderComposer(props: Partial<React.ComponentProps<typeof Composer>> = {}) {
  const onSend = props.onSend ?? vi.fn();
  const onQueueWhileBusy = props.onQueueWhileBusy ?? vi.fn();

  function Harness() {
    const [draft, setDraft] = useState(props.draft ?? "ni");
    const textareaRef = useRef<HTMLTextAreaElement>(null);
    return (
      <Composer
        draft={draft}
        setDraft={setDraft}
        onSend={onSend}
        onAbort={props.onAbort ?? vi.fn()}
        disabled={props.disabled}
        busy={props.busy}
        busyLabel={props.busyLabel}
        busyElapsedMs={props.busyElapsedMs}
        modelLabel={props.modelLabel ?? "deepseek-v4-flash"}
        reasoningEffort={props.reasoningEffort ?? "high"}
        onModelChange={props.onModelChange ?? vi.fn()}
        onEffortChange={props.onEffortChange ?? vi.fn()}
        editMode={props.editMode ?? "review"}
        onEditModeChange={props.onEditModeChange ?? vi.fn()}
        textareaRef={textareaRef}
        slashCommands={props.slashCommands ?? []}
        onMentionQuery={props.onMentionQuery}
        onMentionPreview={props.onMentionPreview}
        onMentionPicked={props.onMentionPicked}
        mentionResults={props.mentionResults}
        workspaceDir={props.workspaceDir}
        queuedSends={props.queuedSends}
        onQueueWhileBusy={onQueueWhileBusy}
        onDequeueSend={props.onDequeueSend}
      />
    );
  }

  render(<Harness />);
  return {
    textarea: screen.getByPlaceholderText(/Type a prompt|输入提示词/) as HTMLTextAreaElement,
    onSend,
    onQueueWhileBusy,
  };
}

describe("dashboard Composer IME handling (#1669)", () => {
  it("does not send when Enter confirms an IME composition", () => {
    const { textarea, onSend } = renderComposer();

    fireEvent.compositionStart(textarea);
    fireEvent.compositionEnd(textarea);
    fireEvent.keyDown(textarea, { key: "Enter" });

    expect(onSend).not.toHaveBeenCalled();
  });

  it("still sends on a normal Enter outside composition", () => {
    const { textarea, onSend } = renderComposer();

    fireEvent.keyDown(textarea, { key: "Enter" });

    expect(onSend).toHaveBeenCalledTimes(1);
  });
});
