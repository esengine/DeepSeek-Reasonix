// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { MarketGroup } from "./Market";
import { MockPort } from "../port/mock";
import { HttpError } from "../port/port";
import type { AgentPort } from "../port/port";

afterEach(cleanup);

const signedIn = { signedIn: true, user: { handle: "demo", email: "demo@example.com", label: "demo" } };
const pinned = "https://github.com/demo/themes/tree/" + "a".repeat(40) + "/dusk";

describe("publishing to the community market", () => {
  it("offers only a way to sign in while signed out", async () => {
    const port = new MockPort() as unknown as AgentPort;
    const onSignIn = vi.fn();
    render(<MarketGroup port={port} onInstalled={() => {}} account={{ signedIn: false }} onSignIn={onSignIn} />);
    expect(document.querySelector('[data-action="market.view"]')).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "去登录" }));
    expect(onSignIn).toHaveBeenCalled();
  });

  it("submits a theme under the account and shows it pending in my packages", async () => {
    const port = new MockPort() as unknown as AgentPort;
    const publish = vi.spyOn(port, "publishMarket");
    render(<MarketGroup port={port} onInstalled={() => {}} account={signedIn} onSignIn={() => {}} />);

    await userEvent.click(screen.getByRole("radio", { name: "发布" }));
    expect(screen.getByText(/以 @demo 的名义提交/)).toBeTruthy();
    const submit = screen.getByRole<HTMLButtonElement>("button", { name: "提交审核" });
    expect(submit.disabled).toBe(true);

    await userEvent.click(screen.getByRole("radio", { name: "主题" }));
    fireEvent.change(document.querySelector('[data-value="name"]')!, { target: { value: "dusk" } });
    fireEvent.change(document.querySelector('[data-value="source"]')!, { target: { value: pinned } });
    fireEvent.change(document.querySelector('[data-value="tags"]')!, { target: { value: "dark， calm, " } });
    expect(submit.disabled).toBe(false);
    await userEvent.click(submit);

    await waitFor(() => expect(publish).toHaveBeenCalled());
    expect(publish.mock.calls[0]![0]).toMatchObject({ kind: "theme", name: "dusk", source: pinned, tags: ["dark", "calm"] });
    expect(await screen.findByText("已提交 demo/dusk 0.1.0")).toBeTruthy();

    await userEvent.click(screen.getByRole("button", { name: "查看我的发布" }));
    const row = (await screen.findByText("dusk")).closest("li")!;
    expect(row.textContent).toContain("审核中");
    expect(row.textContent).toContain("主题");
  });

  it("says why the kernel refused, in the window's words", async () => {
    const port = new MockPort() as unknown as AgentPort;
    vi.spyOn(port, "publishMarket").mockRejectedValue(
      new HttpError(422, "not installable", { code: "market.unpublishable", error: "not installable" }),
    );
    render(<MarketGroup port={port} onInstalled={() => {}} account={signedIn} onSignIn={() => {}} />);
    await userEvent.click(screen.getByRole("radio", { name: "发布" }));
    fireEvent.change(document.querySelector('[data-value="name"]')!, { target: { value: "kit" } });
    fireEvent.change(document.querySelector('[data-value="source"]')!, { target: { value: "https://github.com/demo/kit" } });
    await userEvent.click(screen.getByRole("button", { name: "提交审核" }));
    expect(await screen.findByText(/审核通过后也无法从市场安装/)).toBeTruthy();
  });
});
