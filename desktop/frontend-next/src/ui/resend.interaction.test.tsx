// @vitest-environment jsdom
import "./testkit";
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Item } from "../state/session";
import type { Checkpoint } from "../port/port";
import { UserCard } from "./cards/UserCard";

afterEach(cleanup);

const item = { t: "user", id: "row", text: "把重试次数从 1 改成 3" } as Extract<Item, { t: "user" }>;
const cp: Checkpoint = { turn: 4, prompt: "把重试次数从 1 改成 3", files: 2, msgIndex: 7 };

describe("editing a message and sending it again", () => {
  it("offers nothing without a turn to go back to", () => {
    render(<UserCard item={item} onResend={vi.fn()} />);
    expect(screen.queryByRole("button", { name: /改写/ })).toBeNull();
  });

  it("offers nothing while the line is still queued", () => {
    render(<UserCard item={{ ...item, pending: true }} cp={cp} onResend={vi.fn()} />);
    expect(screen.queryByRole("button", { name: /改写/ })).toBeNull();
  });

  it("opens on the message's own words", async () => {
    render(<UserCard item={item} cp={cp} onResend={vi.fn()} />);
    await userEvent.click(screen.getByRole("button", { name: /改写/ }));
    expect((screen.getByRole("textbox") as HTMLTextAreaElement).value).toBe("把重试次数从 1 改成 3");
  });

  it("sends the edit against the turn the checkpoint claims", async () => {
    const resend = vi.fn(() => Promise.resolve());
    render(<UserCard item={item} cp={cp} onResend={resend} />);
    await userEvent.click(screen.getByRole("button", { name: /改写/ }));
    await userEvent.clear(screen.getByRole("textbox"));
    await userEvent.type(screen.getByRole("textbox"), "改成 5");
    await userEvent.click(screen.getByRole("button", { name: "改完重发" }));
    expect(resend).toHaveBeenCalledWith(4, "改成 5");
  });

  it("refuses to send an empty edit", async () => {
    const resend = vi.fn(() => Promise.resolve());
    render(<UserCard item={item} cp={cp} onResend={resend} />);
    await userEvent.click(screen.getByRole("button", { name: /改写/ }));
    await userEvent.clear(screen.getByRole("textbox"));
    expect((screen.getByRole("button", { name: "改完重发" }) as HTMLButtonElement).disabled).toBe(true);
    expect(resend).not.toHaveBeenCalled();
  });

  it("stays open and says why when the kernel refuses", async () => {
    const resend = vi.fn(() => Promise.reject(new Error("checkpoint is gone")));
    render(<UserCard item={item} cp={cp} onResend={resend} />);
    await userEvent.click(screen.getByRole("button", { name: /改写/ }));
    await userEvent.click(screen.getByRole("button", { name: "改完重发" }));
    await waitFor(() => expect(screen.getByText(/checkpoint is gone/)).toBeTruthy());
    expect(screen.getByRole("textbox")).toBeTruthy();
  });

  it("does not send twice while one send is in flight", async () => {
    let release = () => {};
    const resend = vi.fn(() => new Promise<void>((ok) => { release = ok; }));
    render(<UserCard item={item} cp={cp} onResend={resend} />);
    await userEvent.click(screen.getByRole("button", { name: /改写/ }));
    await userEvent.click(screen.getByRole("button", { name: "改完重发" }));
    const sending = screen.getByRole("button", { name: "正在重发…" }) as HTMLButtonElement;
    expect(sending.disabled).toBe(true);
    await userEvent.click(sending);
    expect(resend).toHaveBeenCalledTimes(1);
    release();
  });

  it("puts the message back when the edit is abandoned", async () => {
    render(<UserCard item={item} cp={cp} onResend={vi.fn()} />);
    await userEvent.click(screen.getByRole("button", { name: /改写/ }));
    await userEvent.click(screen.getByRole("button", { name: "取消" }));
    expect(screen.queryByRole("textbox")).toBeNull();
    expect(screen.getByText("把重试次数从 1 改成 3")).toBeTruthy();
  });
});
