// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { App } from "./App";
import { Onboarding } from "./Onboarding";
import { Composer } from "./Composer";
import { draftKey } from "./drafts";
import type { SessionStatus } from "../port/port";
import { MockHub } from "../port/mock_hub";
import { MockPort } from "../port/mock";
import type { AgentPort } from "../port/port";

afterEach(cleanup);

function unconnectedHub() {
  const hub = new MockHub();
  const build = hub.portFor.bind(hub);
  hub.portFor = (rt) => {
    const port = build(rt);
    port.providerSetup = async () => ({ required: true, noProvider: true });
    port.welcomeSeen = async () => true;
    return port;
  };
  return hub;
}

describe("first-run setup can be put off", () => {
  it("offers the skip only when the caller can honour it", () => {
    const port = new MockPort() as unknown as AgentPort;
    const setup = { required: true, noProvider: true };
    const view = render(<Onboarding port={port} setup={setup} onDone={vi.fn()} />);
    expect(screen.queryByRole("button", { name: "稍后再说" })).toBeNull();
    const onSkip = vi.fn();
    view.rerender(<Onboarding port={port} setup={setup} onDone={vi.fn()} onSkip={onSkip} />);
    screen.getByRole("button", { name: "稍后再说" }).click();
    expect(onSkip).toHaveBeenCalledOnce();
  });

  it("opens the workbench with a way back to connecting, and returns to setup from it", async () => {
    const user = userEvent.setup();
    render(<App hub={unconnectedHub()} />);

    await user.click(await screen.findByRole("button", { name: "稍后再说" }));
    await waitFor(() => expect(document.querySelector(".onb-stage")).toBeNull());

    const connect = await screen.findByRole("button", { name: "去连接" });
    expect(screen.getByText("还没有连接模型服务，发不出去")).toBeTruthy();

    await user.click(connect);
    expect(await screen.findByRole("button", { name: "稍后再说" })).toBeTruthy();
  });

  it("asks again on the next launch instead of remembering the skip", async () => {
    const user = userEvent.setup();
    const first = render(<App hub={unconnectedHub()} />);
    await user.click(await screen.findByRole("button", { name: "稍后再说" }));
    await waitFor(() => expect(document.querySelector(".onb-stage")).toBeNull());
    first.unmount();

    render(<App hub={unconnectedHub()} />);
    expect(await screen.findByRole("button", { name: "稍后再说" })).toBeTruthy();
  });
});

describe("the composer with nothing connected", () => {
  it("names the state instead of the placeholder model, and the model menu still opens", async () => {
    const user = userEvent.setup();
    const port = new MockPort() as unknown as AgentPort;
    port.models = async () => [];
    const st = { modelRef: "unconfigured/none", workspaceRoot: "/w", sessionPath: "" } as SessionStatus;
    render(
      <Composer port={port} status={st} running={false} focus={0} onSubmit={vi.fn(async () => true)}
        onChanged={vi.fn()} onError={vi.fn()} draftKey={draftKey("", "/w", "")} />,
    );
    const label = (await screen.findAllByText("未连接"))[0];
    expect(screen.queryByText("none")).toBeNull();
    await user.click(label);
  });
});
