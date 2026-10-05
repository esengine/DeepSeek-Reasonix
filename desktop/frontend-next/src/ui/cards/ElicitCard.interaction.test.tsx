// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Item } from "../../state/session";
import { ElicitCard } from "./ElicitCard";

afterEach(cleanup);

const pending = () => new Promise<void>((resolve) => setTimeout(resolve, 20));

const form = (extra: Partial<Extract<Item, { t: "ask" }>> = {}) => ({
  t: "ask", id: "row", ...extra, ask: { id: "ask", origin: { kind: "mcp", source: "deployer", message: "Deploy settings", note: "Email is required" }, questions: [
    { id: "email", header: "Email", prompt: "Where to send the report", options: [] },
    { id: "env", header: "env", prompt: "env", multi: false, options: [{ label: "prod" }, { label: "dev" }] },
  ] },
}) as Extract<Item, { t: "ask" }>;

describe("a server's form", () => {
  // The person sees who asks and that it is not the agent, fills the fields in
  // place, and sends them together.
  it("names the server and sends every field at once", async () => {
    const answer = vi.fn(pending);
    render(<ElicitCard item={form()} onAnswer={answer} />);
    expect(screen.getByText("MCP 服务器 deployer 请求信息")).toBeTruthy();
    expect(screen.getByText("Deploy settings")).toBeTruthy();
    expect(screen.getByText("上次填写未通过：Email is required")).toBeTruthy();
    expect((screen.getByRole("button", { name: "提交" }) as HTMLButtonElement).disabled).toBe(true);

    await userEvent.type(screen.getByRole("textbox", { name: "Email" }), "ada@example.com");
    await userEvent.click(screen.getByRole("button", { name: "dev" }));
    await userEvent.click(screen.getByRole("button", { name: "提交" }));
    expect(answer).toHaveBeenCalledWith("row", "ask", [
      { questionId: "email", selected: ["ada@example.com"] },
      { questionId: "env", selected: ["dev"] },
    ]);
  });

  it("declines with nothing given", async () => {
    const answer = vi.fn(pending);
    render(<ElicitCard item={form()} onAnswer={answer} />);
    await userEvent.click(screen.getByRole("button", { name: "拒绝提供" }));
    expect(answer).toHaveBeenCalledWith("row", "ask", [{ questionId: "email", selected: [] }, { questionId: "env", selected: [] }]);
  });

  it("reads back what was sent, or that it was declined", () => {
    render(<ElicitCard item={form({ answered: [["a@b.co"], ["prod"]] })} onAnswer={vi.fn(pending)} />);
    expect(screen.getByText("已提交给 deployer")).toBeTruthy();
    expect(screen.queryByText(/上次填写未通过/)).toBeNull();
    cleanup();
    render(<ElicitCard item={form({ answered: [[], []] })} onAnswer={vi.fn(pending)} />);
    expect(screen.getByText("已拒绝提供")).toBeTruthy();
  });
});

describe("a form sent back", () => {
  it("opens with what was given last time", async () => {
    const answer = vi.fn(pending);
    const item = {
      t: "ask", id: "row", ask: { id: "ask", origin: { kind: "mcp", source: "deployer", note: "\"Replicas\": out of range" }, questions: [
        { id: "replicas", header: "Replicas", prompt: "1 to 10", options: [], default: ["20"] },
        { id: "env", header: "env", prompt: "env", options: [{ label: "prod" }, { label: "dev" }], default: ["dev"] },
      ] },
    } as Extract<Item, { t: "ask" }>;
    render(<ElicitCard item={item} onAnswer={answer} />);
    const replicas = screen.getByRole("textbox", { name: "Replicas" }) as HTMLInputElement;
    expect(replicas.value).toBe("20");
    expect(screen.getByRole("button", { name: "dev" }).getAttribute("aria-pressed")).toBe("true");
    await userEvent.clear(replicas);
    await userEvent.type(replicas, "3");
    await userEvent.click(screen.getByRole("button", { name: "提交" }));
    expect(answer).toHaveBeenCalledWith("row", "ask", [{ questionId: "replicas", selected: ["3"] }, { questionId: "env", selected: ["dev"] }]);
  });
});
