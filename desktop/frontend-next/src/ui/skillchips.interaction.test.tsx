// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import "./testkit";
import { Composer } from "./Composer";
import { MockPort } from "../port/mock";
import type { AgentPort, ChipCall, SessionStatus } from "../port/port";

afterEach(cleanup);

function draw(onSubmit: (text: string, chips?: ChipCall) => Promise<boolean> = vi.fn(async () => true)) {
  const port = new MockPort();
  const view = render(
    <Composer
      port={port as unknown as AgentPort}
      status={{ plan: false, effort: "auto", modelRef: "deepseek/deepseek-v4-pro" } as SessionStatus}
      running={false}
      focus={0}
      onSubmit={onSubmit}
      onChanged={vi.fn()}
      onError={vi.fn()}
    />,
  );
  const box = view.container.querySelector('textarea[aria-label="任务输入"]') as HTMLTextAreaElement;
  const typeIn = (value: string, caret = value.length) => fireEvent.change(box, { target: { value, selectionStart: caret } });
  return { ...view, port, box, typeIn, onSubmit };
}

const chipsOn = (container: HTMLElement) => [...container.querySelectorAll(".chipmirror .skillchip")].map((c) => c.textContent);

describe("skill chips in the composer", () => {
  it("offers only skills after a slash mid-line and sends the pick beside the text", async () => {
    const onSubmit = vi.fn(async (_text: string, _chips?: ChipCall) => true);
    const { box, typeIn, container } = draw(onSubmit);
    typeIn("先看一下 /");
    expect(await screen.findByRole("option", { name: /init/ })).toBeTruthy();
    expect(screen.queryByRole("option", { name: /commit/ })).toBeNull();
    expect(screen.queryByRole("option", { name: /compact/ })).toBeNull();
    typeIn("先看一下 /in");
    await screen.findByRole("option", { name: /init/ });
    fireEvent.keyDown(box, { key: "Enter" });
    await waitFor(() => expect(box.value).toBe("先看一下 /init "));
    expect(chipsOn(container)).toEqual(["/init"]);
    expect(box.hasAttribute("data-chips")).toBe(true);

    typeIn("先看一下 /init 再说");
    fireEvent.keyDown(box, { key: "Enter" });
    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
    expect(onSubmit).toHaveBeenCalledWith("先看一下 /init 再说", {
      submit: "先看一下 再说",
      invocations: [{ name: "init", kind: "skill", offset: 5 }],
    });
  });

  it("turns a leading skill pick into a chip and leaves a command as text", async () => {
    const onSubmit = vi.fn(async (_text: string, _chips?: ChipCall) => true);
    const { box, typeIn, container } = draw(onSubmit);
    typeIn("/rev");
    await screen.findByRole("option", { name: /review/ });
    fireEvent.keyDown(box, { key: "Enter" });
    await waitFor(() => expect(chipsOn(container)).toEqual(["/review"]));
    typeIn("/review 这次改动");
    fireEvent.keyDown(box, { key: "Enter" });
    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith("/review 这次改动", {
      submit: "这次改动",
      invocations: [{ name: "review", kind: "subagent", offset: 0 }],
    }));

    typeIn("/comm");
    await screen.findByRole("option", { name: /commit/ });
    fireEvent.keyDown(box, { key: "Enter" });
    await waitFor(() => expect(box.value).toBe("/commit "));
    expect(chipsOn(container)).toEqual([]);
  });

  it("removes a chip whole when backspace reaches it", async () => {
    const { box, typeIn, container } = draw();
    typeIn("帮我 /in");
    await screen.findByRole("option", { name: /init/ });
    fireEvent.keyDown(box, { key: "Enter" });
    await waitFor(() => expect(chipsOn(container)).toEqual(["/init"]));
    typeIn("帮我 /ini ", 7);
    await waitFor(() => expect(box.value).toBe("帮我  "));
    expect(chipsOn(container)).toEqual([]);
    expect(box.hasAttribute("data-chips")).toBe(false);
  });

  it("gives the chips back with the draft when the line is refused", async () => {
    let settle!: (sent: boolean) => void;
    const { box, typeIn, container } = draw(() => new Promise<boolean>((done) => (settle = done)));
    typeIn("/in");
    await screen.findByRole("option", { name: /init/ });
    fireEvent.keyDown(box, { key: "Enter" });
    await waitFor(() => expect(chipsOn(container)).toEqual(["/init"]));
    fireEvent.keyDown(box, { key: "Enter" });
    await waitFor(() => expect(box.value).toBe(""));
    await act(async () => settle(false));
    await waitFor(() => expect(box.value).toBe("/init "));
    expect(chipsOn(container)).toEqual(["/init"]);
  });
});
