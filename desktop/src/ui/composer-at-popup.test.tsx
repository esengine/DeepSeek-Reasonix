// @vitest-environment jsdom

import { fireEvent, render, waitFor } from "@testing-library/react";
import { createRef } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { setLang } from "../i18n";
import { Composer } from "./composer";

vi.mock("@tauri-apps/plugin-dialog", () => ({ open: vi.fn() }));
afterEach(() => {
  vi.clearAllMocks();
});

function renderComposer(props?: Partial<React.ComponentProps<typeof Composer>>) {
  const textareaRef = createRef<HTMLTextAreaElement>();
  const onMentionQuery = vi.fn();
  const utils = render(
    <Composer
      draft=""
      setDraft={vi.fn()}
      onSend={vi.fn()}
      onAbort={vi.fn()}
      disabled={false}
      busy={false}
      modelLabel="deepseek-v4-flash"
      reasoningEffort="high"
      onModelChange={vi.fn()}
      onEffortChange={vi.fn()}
      editMode="review"
      onEditModeChange={vi.fn()}
      textareaRef={textareaRef}
      slashCommands={[]}
      onMentionQuery={onMentionQuery}
      onMentionPreview={vi.fn()}
      onMentionPicked={vi.fn()}
      mentionResults={null}
      workspaceDir="/repo"
      {...props}
    />,
  );

  return { ...utils, textareaRef, onMentionQuery };
}

describe("desktop Composer @ popup", () => {
  beforeEach(() => {
    setLang("en");
  });

  it("keeps the active mention row when async results shrink", async () => {
    const { container, rerender, onMentionQuery } = renderComposer();
    const textarea = container.querySelector("textarea");
    if (!textarea) throw new Error("missing textarea");

    fireEvent.change(textarea, { target: { value: "@re" } });

    await waitFor(() => expect(onMentionQuery).toHaveBeenCalled());
    const nonce = onMentionQuery.mock.calls[0]?.[1] as number;

    rerender(
      <Composer
        draft="@re"
        setDraft={vi.fn()}
        onSend={vi.fn()}
        onAbort={vi.fn()}
        disabled={false}
        busy={false}
        modelLabel="deepseek-v4-flash"
        reasoningEffort="high"
        onModelChange={vi.fn()}
        onEffortChange={vi.fn()}
        editMode="review"
        onEditModeChange={vi.fn()}
        textareaRef={createRef<HTMLTextAreaElement>()}
        slashCommands={[]}
        onMentionQuery={onMentionQuery}
        onMentionPreview={vi.fn()}
        onMentionPicked={vi.fn()}
        mentionResults={{ nonce, query: "re", results: ["alpha", "beta", "gamma"] }}
        workspaceDir="/repo"
      />,
    );

    await waitFor(() => {
      expect(container.querySelector('.popup-item[data-active="true"]')?.textContent).toContain(
        "alpha",
      );
    });

    const items = container.querySelectorAll(".popup-item");
    fireEvent.mouseEnter(items[1]!);

    expect(container.querySelector('.popup-item[data-active="true"]')?.textContent).toContain(
      "beta",
    );

    rerender(
      <Composer
        draft="@re"
        setDraft={vi.fn()}
        onSend={vi.fn()}
        onAbort={vi.fn()}
        disabled={false}
        busy={false}
        modelLabel="deepseek-v4-flash"
        reasoningEffort="high"
        onModelChange={vi.fn()}
        onEffortChange={vi.fn()}
        editMode="review"
        onEditModeChange={vi.fn()}
        textareaRef={createRef<HTMLTextAreaElement>()}
        slashCommands={[]}
        onMentionQuery={onMentionQuery}
        onMentionPreview={vi.fn()}
        onMentionPicked={vi.fn()}
        mentionResults={{ nonce, query: "re", results: ["alpha", "beta"] }}
        workspaceDir="/repo"
      />,
    );

    await waitFor(() => {
      expect(container.querySelector('.popup-item[data-active="true"]')?.textContent).toContain(
        "beta",
      );
    });
  });

  it("uses the wider at popup class for mention results", async () => {
    const { container, rerender, onMentionQuery } = renderComposer();
    const textarea = container.querySelector("textarea");
    if (!textarea) throw new Error("missing textarea");

    fireEvent.change(textarea, { target: { value: "@re" } });
    await waitFor(() => expect(onMentionQuery).toHaveBeenCalled());
    const nonce = onMentionQuery.mock.calls[0]?.[1] as number;

    rerender(
      <Composer
        draft="@re"
        setDraft={vi.fn()}
        onSend={vi.fn()}
        onAbort={vi.fn()}
        disabled={false}
        busy={false}
        modelLabel="deepseek-v4-flash"
        reasoningEffort="high"
        onModelChange={vi.fn()}
        onEffortChange={vi.fn()}
        editMode="review"
        onEditModeChange={vi.fn()}
        textareaRef={createRef<HTMLTextAreaElement>()}
        slashCommands={[]}
        onMentionQuery={onMentionQuery}
        onMentionPreview={vi.fn()}
        onMentionPicked={vi.fn()}
        mentionResults={{ nonce, query: "re", results: ["alpha"] }}
        workspaceDir="/repo"
      />,
    );

    expect(container.querySelector(".popup-list.at-popup-list")).not.toBeNull();
  });

  it("delegates ArrowUp prompt-history navigation only at the input start", () => {
    const onPromptHistoryNavigate = vi.fn(() => true);
    const { container } = renderComposer({
      draft: "current prompt",
      onPromptHistoryNavigate,
    });
    const textarea = container.querySelector("textarea");
    if (!textarea) throw new Error("missing textarea");

    textarea.setSelectionRange(3, 3);
    fireEvent.keyDown(textarea, { key: "ArrowUp" });
    expect(onPromptHistoryNavigate).not.toHaveBeenCalled();

    textarea.setSelectionRange(0, 0);
    fireEvent.keyDown(textarea, { key: "ArrowUp" });
    expect(onPromptHistoryNavigate).toHaveBeenCalledWith("older");
  });

  it("allows ArrowDown prompt-history navigation while browsing at a boundary", () => {
    const onPromptHistoryNavigate = vi.fn(() => true);
    const { container } = renderComposer({
      draft: "recalled prompt",
      promptHistoryBrowsing: true,
      onPromptHistoryNavigate,
    });
    const textarea = container.querySelector("textarea");
    if (!textarea) throw new Error("missing textarea");

    textarea.setSelectionRange("recalled prompt".length, "recalled prompt".length);
    fireEvent.keyDown(textarea, { key: "ArrowDown" });

    expect(onPromptHistoryNavigate).toHaveBeenCalledWith("newer");
  });
});
