import { Text, render } from "ink";
import React, { useEffect } from "react";
import { afterEach, describe, expect, it } from "vitest";
import { InputAreaWithHistoryHint } from "../src/cli/ui/layout/InputAreaWithHistoryHint.js";
import {
  ChatScrollProvider,
  useChatScrollActions,
} from "../src/cli/ui/state/chat-scroll-provider.js";
import { setLanguageRuntime } from "../src/i18n/index.js";
import { makeFakeStdin, makeFakeStdout } from "./helpers/ink-stdio.js";

function ScrollAwayFromBottom(): null {
  const scroll = useChatScrollActions();
  useEffect(() => {
    scroll.setMaxScroll(10);
    scroll.scrollUp();
  }, [scroll]);
  return null;
}

async function renderHistoryHint(lang: "EN" | "zh-CN"): Promise<string> {
  setLanguageRuntime(lang);
  const stdout = makeFakeStdout();
  const { unmount } = render(
    <ChatScrollProvider>
      <ScrollAwayFromBottom />
      <InputAreaWithHistoryHint inputArea={<Text>input</Text>} />
    </ChatScrollProvider>,
    { stdout: stdout as never, stdin: makeFakeStdin() as never },
  );
  await new Promise((resolve) => setTimeout(resolve, 50));
  unmount();
  return stdout.text();
}

describe("InputAreaWithHistoryHint", () => {
  afterEach(() => {
    setLanguageRuntime("EN");
  });

  it("renders a readable zh-CN history hint when the chat is scrolled up", async () => {
    const text = await renderHistoryHint("zh-CN");

    expect(text).toContain("正在查看历史");
    expect(text).toContain("End / PgDn");
    expect(text).not.toMatch(/闂\?|濠电姷|鏁告|鈥|�/);
  });
});
