// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { App } from "./App";
import { dropDraft } from "./feedbackdraft";
import { MockHub } from "../port/mock_hub";

afterEach(() => {
  cleanup();
  dropDraft();
});

function paneless(opts: { folders: boolean }) {
  const hub = new MockHub();
  const build = hub.portFor.bind(hub);
  hub.portFor = (rt) => {
    const port = build(rt);
    port.providerSetup = async () => null;
    port.welcomeSeen = async () => true;
    return port;
  };
  const open = hub.open.bind(hub);
  const opened: Array<string | undefined> = [];
  let live = false;
  hub.runtimes = async () => (live ? [await open({ root: "~/projects/DeepSeek-Reasonix" })] : []);
  hub.open = async (req) => {
    live = true;
    opened.push(req.root);
    return open(req);
  };
  if (!opts.folders) hub.tree = async () => [];
  return { hub, opened };
}

describe("feedback and settings with no pane open", () => {
  it("opens the feedback form by starting a session in the current folder", async () => {
    const { hub, opened } = paneless({ folders: true });
    render(<App hub={hub} />);
    await userEvent.click(await screen.findByRole("button", { name: /发送反馈/ }));
    expect(await screen.findByRole("dialog", { name: "反馈" })).toBeTruthy();
    expect(opened).toEqual(["~/projects/DeepSeek-Reasonix"]);
    expect(document.querySelector(".errbar")).toBeNull();
  });

  it("opens the feedback form in the app's own workspace when there is no folder", async () => {
    const { hub, opened } = paneless({ folders: false });
    render(<App hub={hub} />);
    await userEvent.click(await screen.findByRole("button", { name: /发送反馈/ }));
    expect(await screen.findByRole("dialog", { name: "反馈" })).toBeTruthy();
    expect(opened).toEqual([undefined]);
    expect(document.querySelector(".errbar")).toBeNull();
  });

  it("opens settings in the app's own workspace when there is no folder", async () => {
    const { hub, opened } = paneless({ folders: false });
    render(<App hub={hub} />);
    await userEvent.click(await screen.findByRole("button", { name: /扩展/ }));
    await waitFor(() => expect(opened).toEqual([undefined]));
    expect(document.querySelector(".errbar")).toBeNull();
  });
});
