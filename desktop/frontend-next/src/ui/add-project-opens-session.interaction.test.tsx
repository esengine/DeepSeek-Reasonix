// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { App } from "./App";
import { MockHub } from "../port/mock_hub";

afterEach(cleanup);

describe("adding a project from the sidebar", () => {
  it("selects it and opens a new session in it, without sending anything", async () => {
    const user = userEvent.setup();
    const hub = new MockHub();
    const build = hub.portFor.bind(hub);
    hub.portFor = (rt) => {
      const port = build(rt);
      port.providerSetup = async () => null;
      port.welcomeSeen = async () => true;
      return port;
    };
    hub.pickFolder = async () => "/work/fresh-project";
    const open = vi.spyOn(hub, "open");
    render(<App hub={hub} />);

    await user.click((await screen.findAllByRole("button", { name: "打开项目" }))[0]);
    await user.click(await screen.findByRole("button", { name: "选择本机文件夹" }));

    await waitFor(() => expect(open).toHaveBeenCalledWith({ root: "/work/fresh-project" }));
  });
});
