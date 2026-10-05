// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { MarketVote, approvalLabel } from "./MarketVote";
import { MockPort } from "../port/mock";
import { HttpError, type AgentPort, type MarketPackage, type MarketVote as Vote } from "../port/port";

afterEach(cleanup);

const pkg: MarketPackage = {
  kind: "skill", handle: "acme", name: "kit", slug: "acme/kit", summary: "", description: "", homepage: "", repoUrl: "",
  tags: [], latestVersion: "1.0.0", installCount: 10, starCount: 3, upCount: 3, downCount: 1, approvalRate: 0.75, score: 0.5,
  verified: false, status: "active", updatedAt: "",
};

function portWith(mine: Vote | Error) {
  const port = new MockPort() as unknown as AgentPort;
  port.marketMyVote = vi.fn(async () => {
    if (mine instanceof Error) throw mine;
    return mine;
  });
  port.voteMarket = vi.fn(async (_slug: string, value: -1 | 0 | 1) => ({
    signedIn: true, value, upCount: value === 1 ? 4 : 3, downCount: 1, approvalRate: value === 1 ? 0.8 : 0.75, canVote: true,
  }));
  return port;
}

const up = () => screen.getByRole("button", { name: /赞/ });

describe("market votes", () => {
  it("lights the signed-in vote, withdraws it on a second press and casts the other", async () => {
    const port = portWith({ signedIn: true, value: 1, upCount: 3, downCount: 1, approvalRate: 0.75, canVote: true });
    render(<MarketVote port={port} pkg={pkg} />);
    await waitFor(() => expect(up().getAttribute("aria-pressed")).toBe("true"));
    await userEvent.click(up());
    expect(port.voteMarket).toHaveBeenLastCalledWith("acme/kit", 0);
    await waitFor(() => expect(up().getAttribute("aria-pressed")).toBe("false"));
    await userEvent.click(screen.getByRole("button", { name: /踩/ }));
    expect(port.voteMarket).toHaveBeenLastCalledWith("acme/kit", -1);
  });

  it("offers sign-in instead of voting when signed out", async () => {
    const port = portWith({ signedIn: false, value: 0 });
    const onSignIn = vi.fn();
    render(<MarketVote port={port} pkg={pkg} onSignIn={onSignIn} />);
    await userEvent.click(await screen.findByRole("button", { name: "登录后评价" }));
    expect(onSignIn).toHaveBeenCalled();
    expect(up().hasAttribute("disabled")).toBe(true);
    expect(port.voteMarket).not.toHaveBeenCalled();
  });

  it("says why a publisher cannot vote on their own package", async () => {
    const port = portWith({ signedIn: true, value: 0, upCount: 3, downCount: 1, approvalRate: 0.75, canVote: false, own: true });
    render(<MarketVote port={port} pkg={pkg} />);
    expect(await screen.findByText("不能评价自己发布的包")).toBeTruthy();
    expect(up().hasAttribute("disabled")).toBe(true);
  });

  it("shows only the tally where the account routes are shut", async () => {
    const port = portWith(new HttpError(403, "x", { code: "account.signin_disabled" }));
    render(<MarketVote port={port} pkg={pkg} onSignIn={() => {}} />);
    expect(await screen.findByText("在这台设备上不能评价")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "登录后评价" })).toBeNull();
    expect(screen.getByText("好评 75%（4 票）")).toBeTruthy();
  });

  it("reads no votes as no share rather than zero", () => {
    expect(approvalLabel({ approvalRate: null, upCount: 0, downCount: 0 })).toBe("暂无评价");
    expect(approvalLabel({ approvalRate: 0, upCount: 0, downCount: 2 })).toBe("好评 0%（2 票）");
  });
});
