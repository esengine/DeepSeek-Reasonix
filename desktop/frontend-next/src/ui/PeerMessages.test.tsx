// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { PeerMessages } from "./PeerMessages";
import { usePeersSettings } from "./usePeersSettings";
import { PeerHeldBar } from "./PeerHeldBar";
import { MockPort } from "../port/mock";
import type { AgentPort, PeersState } from "../port/port";

function Messages({ port }: { port: Parameters<typeof PeerMessages>[0]["port"] }) {
  return <PeerMessages port={port} settings={usePeersSettings(port)} />;
}

afterEach(cleanup);

const held = { id: "m-1", fromSession: "s-1", fromTitle: "拾音", fromProject: "alpha", sentAt: "2026-10-16T12:00:00Z", body: "请核对第三节" };
const peer = { key: "s-2", title: "文档整理", project: "docs" };

class PeerPort extends MockPort {
  constructor(state: PeersState) {
    super();
    this.peerState = state;
  }
}
const port = (state: PeersState) => new PeerPort(state) as unknown as AgentPort;

describe("messages between sessions", () => {
  it("offers the switch to turn it on, and no sending, when the kernel has it disabled", async () => {
    render(<Messages port={port({ enabled: false, peers: [], held: [] })} />);
    expect(await screen.findByRole("switch", { name: "启用会话间消息" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "发送" })).toBeNull();
  });

  it("sends the text to the chosen session and says the other user still decides", async () => {
    const p = port({ enabled: true, peers: [peer], held: [] });
    const send = vi.spyOn(p, "sendPeerMessage");
    render(<Messages port={p} />);
    const box = await screen.findByLabelText("要发的内容");
    const button = screen.getByRole("button", { name: "发送" });
    expect(button.hasAttribute("disabled")).toBe(true);
    await userEvent.type(box, "迁移完成");
    await userEvent.click(button);
    expect(send).toHaveBeenCalledWith("s-2", "迁移完成");
    expect(await screen.findByText(/对方的模型会直接读到/)).toBeTruthy();
    expect((box as HTMLTextAreaElement).value).toBe("");
  });

  it("shows the refusal when the kernel refuses a send and keeps the words", async () => {
    const p = port({ enabled: true, peers: [peer], held: [] });
    p.sendPeerMessage = async () => {
      throw new Error("too many messages sent in a short time");
    };
    render(<Messages port={p} />);
    const box = await screen.findByLabelText("要发的内容");
    await userEvent.type(box, "再来一条");
    await userEvent.click(screen.getByRole("button", { name: "发送" }));
    await screen.findByText(/too many messages/);
    expect((box as HTMLTextAreaElement).value).toBe("再来一条");
  });

  it("holds a received message as another session's words until it is approved or refused", async () => {
    const p = port({ enabled: true, peers: [], held: [held] });
    const resolve = vi.spyOn(p, "resolvePeerMessage");
    render(<Messages port={p} />);
    expect(await screen.findByText(/不是你写的/)).toBeTruthy();
    expect(screen.getByText("请核对第三节")).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "批准并交给模型" }));
    expect(resolve).toHaveBeenCalledWith("m-1", true, "none");
    await waitFor(() => expect(screen.queryByText("请核对第三节")).toBeNull());
  });

  it("refuses without delivering", async () => {
    const p = port({ enabled: true, peers: [], held: [held] });
    const resolve = vi.spyOn(p, "resolvePeerMessage");
    render(<Messages port={p} />);
    await userEvent.click(await screen.findByRole("button", { name: "拒绝" }));
    expect(resolve).toHaveBeenCalledWith("m-1", false, "none");
  });
});

describe("the held-message bar above the composer", () => {
  it("draws nothing when peers are off or nothing is held", async () => {
    const { container, rerender } = render(<PeerHeldBar port={port({ enabled: false, peers: [], held: [held] })} />);
    await waitFor(() => expect(container.querySelector(".peerbar")).toBeNull());
    rerender(<PeerHeldBar port={port({ enabled: true, peers: [], held: [] })} />);
    await waitFor(() => expect(container.querySelector(".peerbar")).toBeNull());
  });

  it("shows the first held message with its decision buttons and counts the rest", async () => {
    render(<PeerHeldBar port={port({ enabled: true, peers: [], held: [held, { ...held, id: "m-2", body: "第二条" }] })} />);
    expect(await screen.findByText("请核对第三节")).toBeTruthy();
    expect(screen.getByText(/还有 1 条等你决定/)).toBeTruthy();
    expect(screen.queryByText("第二条")).toBeNull();
  });
});
