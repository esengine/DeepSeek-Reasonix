// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { MarketGroup } from "./Market";
import { MockPort } from "../port/mock";
import type { AccountState, AgentPort, MarketPublished } from "../port/port";

afterEach(cleanup);

const account = (handle = "demo"): AccountState => ({
  signedIn: true, user: { handle, email: `${handle}@example.com`, label: handle },
});
const draw = (port: AgentPort, handle = "demo") => (
  <MarketGroup port={port} account={account(handle)} onInstalled={() => {}} onSignIn={() => {}} />
);
const field = (name: string) => screen.getByLabelText<HTMLInputElement | HTMLTextAreaElement>(new RegExp(`^${name}`));
const fill = (name: string) => {
  fireEvent.change(field("名称"), { target: { value: name } });
  fireEvent.change(field("来源地址"), { target: { value: `https://github.com/demo/${name}` } });
};
function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

it("preserves a draft while the same connection and account are refreshed", async () => {
  const port = new MockPort() as unknown as AgentPort;
  const view = render(draw(port));
  await userEvent.click(screen.getByRole("radio", { name: "发布" }));
  fill("draft-kit");
  view.rerender(draw(port));
  expect(field("名称").value).toBe("draft-kit");
  expect(field("来源地址").value).toBe("https://github.com/demo/draft-kit");
});

it.each(["connection", "account"])("clears every draft field when the publishing %s changes", async (owner) => {
  const port = new MockPort() as unknown as AgentPort;
  const next = new MockPort() as unknown as AgentPort;
  const publish = vi.spyOn(next, "publishMarket");
  const view = render(draw(port));
  await userEvent.click(screen.getByRole("radio", { name: "发布" }));
  await userEvent.click(screen.getByRole("radio", { name: "插件" }));
  fill("private-kit");
  for (const label of ["版本", "摘要", "描述", "仓库", "标签"]) fireEvent.change(field(label), { target: { value: "old-draft" } });
  await userEvent.click(screen.getByRole("checkbox"));
  view.rerender(draw(owner === "connection" ? next : port, owner === "account" ? "other" : "demo"));
  for (const label of ["名称", "来源地址", "版本", "摘要", "描述", "仓库", "标签"]) expect(field(label).value).toBe("");
  expect(screen.getByRole<HTMLInputElement>("checkbox").checked).toBe(false);
  expect(screen.getByRole("radio", { name: "技能" }).getAttribute("aria-checked")).toBe("true");
  expect(screen.getByRole<HTMLButtonElement>("button", { name: "提交审核" }).disabled).toBe(true);
  expect(publish).not.toHaveBeenCalled();
});

it.each(["connection", "account"])("discards a completed submission when the publishing %s changes", async (owner) => {
  const port = new MockPort() as unknown as AgentPort;
  const next = new MockPort() as unknown as AgentPort;
  const view = render(draw(port));
  await userEvent.click(screen.getByRole("radio", { name: "发布" }));
  fill("old-kit");
  await userEvent.click(screen.getByRole("button", { name: "提交审核" }));
  await screen.findByText("已提交 demo/old-kit 0.1.0");
  expect(screen.getByRole("status").textContent).toContain("已提交 demo/old-kit 0.1.0");
  view.rerender(draw(owner === "connection" ? next : port, owner === "account" ? "other" : "demo"));
  expect(screen.queryByText("已提交 demo/old-kit 0.1.0")).toBeNull();
  expect(screen.queryByRole("button", { name: "查看我的发布" })).toBeNull();
  expect(field("名称").value).toBe("");
  expect(screen.queryByRole("status")).toBeNull();
  expect(screen.queryByRole("alert")).toBeNull();
});

it("discards a previous connection's submission error", async () => {
  const port = new MockPort() as unknown as AgentPort;
  const next = new MockPort() as unknown as AgentPort;
  vi.spyOn(port, "publishMarket").mockRejectedValueOnce(new Error("old submission refused"));
  const view = render(draw(port));
  await userEvent.click(screen.getByRole("radio", { name: "发布" }));
  fill("old-kit");
  await userEvent.click(screen.getByRole("button", { name: "提交审核" }));
  await screen.findByText("old submission refused");
  expect(screen.getByRole("alert").textContent).toContain("old submission refused");
  view.rerender(draw(next));
  expect(screen.queryByText("old submission refused")).toBeNull();
  expect(screen.queryByText("没有提交成功")).toBeNull();
  expect(screen.queryByRole("alert")).toBeNull();
});

it.each([
  ["connection", "success"], ["connection", "failure"], ["account", "success"], ["account", "failure"],
])("ignores the previous %s's pending %s after returning to that owner", async (owner, outcome) => {
  const port = new MockPort() as unknown as AgentPort;
  const next = new MockPort() as unknown as AgentPort;
  const out = await port.publishMarket({ kind: "skill", name: "old-kit", source: "https://github.com/demo/old-kit" });
  const old = deferred<MarketPublished>();
  const fresh = deferred<MarketPublished>();
  const publish = vi.spyOn(port, "publishMarket").mockImplementationOnce(() => old.promise).mockImplementationOnce(() => fresh.promise);
  const view = render(draw(port));
  await userEvent.click(screen.getByRole("radio", { name: "发布" }));
  fill("old-kit");
  await userEvent.click(screen.getByRole("button", { name: "提交审核" }));
  view.rerender(draw(owner === "connection" ? next : port, owner === "account" ? "other" : "demo"));
  view.rerender(draw(port));
  fill("new-kit");
  await userEvent.click(screen.getByRole("button", { name: "提交审核" }));
  expect(publish).toHaveBeenCalledTimes(2);
  expect(publish).toHaveBeenLastCalledWith(expect.objectContaining({ name: "new-kit", source: "https://github.com/demo/new-kit", visibility: "public" }));
  const applying = screen.getByRole<HTMLButtonElement>("button", { name: "提交中…" });
  await act(async () => {
    if (outcome === "success") old.resolve(out);
    else old.reject(new Error("old late refusal"));
  });
  expect(screen.queryByText("已提交 demo/old-kit 0.1.0")).toBeNull();
  expect(screen.queryByText("old late refusal")).toBeNull();
  expect(screen.queryByRole("alert")).toBeNull();
  expect(screen.queryByRole("status")).toBeNull();
  expect(field("名称").value).toBe("new-kit");
  expect(applying.disabled).toBe(true);
  await act(async () => fresh.resolve({ ...out, package: { ...out.package, name: "new-kit", slug: "demo/new-kit" } }));
  await screen.findByText("已提交 demo/new-kit 0.1.0");
  expect(screen.getByRole("status").textContent).toContain("已提交 demo/new-kit 0.1.0");
});
