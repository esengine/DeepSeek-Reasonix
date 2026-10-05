// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { App } from "./App";
import { MockHub } from "../port/mock_hub";

afterEach(cleanup);

// The second folder the fixture knows. Working in it is what focuses it, and a
// new session from the head has to land there rather than in the first row.
const SECOND = "~/projects/my-website";

// Where a new session lands is the whole claim: the fake kernel records which
// folder every open asked for, so a head button reading a different answer than
// the workspace on screen shows up as the wrong folder rather than as styling.
describe("a new session opens in the workspace on screen", () => {
  it("does not fall back to the first folder in the tree", async () => {
    const hub = new MockHub();
    const asked: (string | undefined)[] = [];
    const open = hub.open.bind(hub);
    hub.open = async (req) => {
      asked.push(req.root);
      return open(req);
    };
    // Past onboarding: a window still asking for a key draws neither the rail
    // nor a panes area, and this is a claim about a window that is working.
    const build = hub.portFor.bind(hub);
    hub.portFor = (rt) => {
      const port = build(rt);
      port.providerSetup = async () => null;
      port.welcomeSeen = async () => true;
      return port;
    };

    render(<App hub={hub} />);

    // The tree is the only place a workspace is named now. Folders other than
    // the focused one open shut, so this is the whole way in: unfold, then work
    // in it.
    await userEvent.click(await screen.findByRole("treeitem", { name: /my-website/ }));
    await userEvent.click(await screen.findByRole("treeitem", { name: /站点改版/ }));
    await waitFor(() => expect(asked).toEqual([SECOND]));

    asked.length = 0;
    // The button in the rail's head, which is the one that has no folder of its
    // own to belong to — the rows below name theirs.
    const head = document.querySelector(".studio-rail-head") as HTMLElement;
    await userEvent.click(within(head).getByRole("button", { name: /新建会话/ }));
    await waitFor(() => expect(asked).toEqual([SECOND]));
  });

  it("keeps a conversation's composer mounted while another is in front", async () => {
    const hub = new MockHub();
    const build = hub.portFor.bind(hub);
    hub.portFor = (rt) => {
      const port = build(rt);
      port.providerSetup = async () => null;
      port.welcomeSeen = async () => true;
      return port;
    };

    render(<App hub={hub} />);

    const composer = await screen.findByRole("combobox", { name: "任务输入" });
    await userEvent.type(composer, "keep this draft");
    await userEvent.click(screen.getByRole("treeitem", { name: /上一次的会话/ }));
    await waitFor(() => expect(screen.getByRole("combobox", { name: "任务输入" })).not.toBe(composer));
    await userEvent.click(screen.getByRole("treeitem", { name: /并行会话演示/ }));

    expect((await screen.findByRole("combobox", { name: "任务输入" }) as HTMLTextAreaElement).value).toBe("keep this draft");
  });
});
