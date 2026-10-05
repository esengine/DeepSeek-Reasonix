// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { MockPort } from "../port/mock";
import type { AgentPort, ProviderDraft } from "../port/port";
import { Onboarding } from "./Onboarding";

afterEach(cleanup);

describe("the protocol confirmed during first-run setup", () => {
  it("shows every protocol returned by the probe and saves the selected one", async () => {
    const user = userEvent.setup();
    const port = new MockPort() as unknown as AgentPort;
    const save = vi.fn(async (_draft: ProviderDraft) => {});
    port.saveProvider = save;

    render(
      <Onboarding
        port={port}
        setup={{ required: true, provider: "", model: "" }}
        onDone={vi.fn()}
      />,
    );

    await user.type(screen.getByPlaceholderText("sk-…"), "sk-test");
    await user.click(screen.getByRole("button", { name: "连接并继续" }));

    expect(screen.getByRole("radio", { name: /OpenAI 兼容/ }).getAttribute("aria-checked")).toBe("true");
    const anthropic = screen.getByRole("radio", { name: /Anthropic 兼容/ });
    expect((anthropic as HTMLButtonElement).disabled).toBe(false);
    await user.click(anthropic);
    expect(anthropic.getAttribute("aria-checked")).toBe("true");
    expect((screen.getByLabelText("服务地址") as HTMLInputElement).value).toBe("https://api.deepseek.com/anthropic");

    await user.click(screen.getByRole("button", { name: "开始" }));
    expect(save).toHaveBeenCalledWith(expect.objectContaining({
      kind: "anthropic",
      baseUrl: "https://api.deepseek.com/anthropic",
      // A fresh install already names the deepseek entry this fills in, and the
      // kernel refuses a same-named add that does not say it replaces.
      replace: true,
    }));
  });
});
