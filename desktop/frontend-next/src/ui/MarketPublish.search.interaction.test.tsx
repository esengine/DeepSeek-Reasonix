// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { MyPackages } from "./MarketPublish";
import { MockPort } from "../port/mock";
import type { AgentPort, MarketPackage } from "../port/port";
import { boot, STORAGE, t } from "../i18n";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  localStorage.setItem(STORAGE, "zh");
  boot();
});

const review: MarketPackage = {
  kind: "plugin", handle: "demo", name: "review-kit", slug: "demo/review-kit",
  summary: "Release checklist", description: "", homepage: "", repoUrl: "",
  tags: ["Café", "team"], latestVersion: "0.2.0", installCount: 0,
  starCount: 0, upCount: 0, downCount: 0, approvalRate: null, score: 0,
  verified: false, status: "private", updatedAt: "",
};
const design: MarketPackage = {
  ...review, name: "design-kit", slug: "demo/design-kit", summary: "Colors for a desk",
  tags: ["design"], status: "active", installed: { version: "0.2.0", contentHash: "a".repeat(64) },
};
const rows = [review, design];

function pending<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

async function setup(lang = "zh") {
  localStorage.setItem(STORAGE, lang);
  boot();
  const port = new MockPort() as unknown as AgentPort;
  const read = vi.spyOn(port, "myMarket").mockResolvedValue(rows);
  const submit = vi.spyOn(port, "submitMarket");
  const plan = vi.spyOn(port, "planOwnMarket");
  const install = vi.spyOn(port, "installOwnMarket");
  const props = { port, onInstalled: vi.fn(), onPublish: vi.fn() };
  const view = render(<MyPackages {...props} />);
  await screen.findByText(review.name);
  const user = userEvent.setup();
  return { port, read, submit, plan, install, props, view, user,
    search: screen.getByRole<HTMLInputElement>("searchbox", { name: t("搜索我的发布") }) };
}

it.each(["REVIEW", "demo/review", "checklist", "TEAM", "CAFE\u0301", "team release", "  review\t café  "])(
  "finds an own package by %s without another read or management action", async (query) => {
    const { user, search, read, submit, plan, install, props } = await setup();
    await user.type(search, query);
    expect(screen.getAllByRole("listitem")).toHaveLength(1);
    expect(screen.getByText(review.name)).toBeTruthy();
    expect(screen.queryByText(design.name)).toBeNull();
    expect(screen.getByText(t("私有"))).toBeTruthy();
    expect(read).toHaveBeenCalledTimes(1);
    expect(submit).not.toHaveBeenCalled();
    expect(plan).not.toHaveBeenCalled();
    expect(install).not.toHaveBeenCalled();
    expect(props.onPublish).not.toHaveBeenCalled();
  },
);

it.each(["zh", "en"])("clears a %s empty result with the keyboard and returns focus to search", async (lang) => {
  const { user, search } = await setup(lang);
  if (lang === "en") expect(search.getAttribute("aria-label")).toBe("Search my packages");
  await user.type(search, "absent");
  expect(screen.getByRole("status").textContent).toBe(t("没有找到匹配的包。"));
  expect(screen.queryByText(t("还没有发布过。"))).toBeNull();
  expect(screen.queryAllByRole("listitem")).toHaveLength(0);
  screen.getByRole("button", { name: t("清除搜索") }).focus();
  await user.keyboard("{Enter}");
  expect(search.value).toBe("");
  expect(document.activeElement).toBe(search);
  expect(screen.getAllByRole("listitem")).toHaveLength(2);
  expect(screen.getByText(t("已安装"))).toBeTruthy();
});

it("treats whitespace as an unfiltered list", async () => {
  const { user, search } = await setup();
  await user.type(search, "  ");
  expect(screen.getAllByRole("listitem")).toHaveLength(2);
  expect(screen.queryByRole("status")).toBeNull();
});

it.each(["success", "failure"])("preserves a submission %s while its row is filtered away", async (outcome) => {
  const { user, search, submit, read } = await setup();
  const request = pending<MarketPackage>();
  submit.mockImplementationOnce(() => request.promise);
  await user.click(within(screen.getByText(review.name).closest("li")!).getByRole("button", { name: t("提交审核") }));
  await user.type(search, "design");
  expect(screen.queryByText(review.name)).toBeNull();
  await act(async () => {
    if (outcome === "success") {
      read.mockResolvedValue([{ ...review, status: "pending" }, design]);
      request.resolve({ ...review, status: "pending" });
    } else request.reject(new Error("registry unavailable"));
  });
  await screen.findByText(design.name);
  await user.click(screen.getByRole("button", { name: t("清除搜索") }));
  const row = within(screen.getByText(review.name).closest("li")!);
  expect(submit).toHaveBeenCalledExactlyOnceWith(review.slug);
  if (outcome === "success") {
    expect(row.getByText(t("审核中"))).toBeTruthy();
    expect(row.queryByRole("button", { name: t("提交审核") })).toBeNull();
    expect(read).toHaveBeenCalledTimes(2);
  } else {
    expect(row.getByRole("alert").textContent).toBe("registry unavailable");
    submit.mockResolvedValueOnce({ ...review, status: "pending" });
    await user.click(row.getByRole("button", { name: t("提交审核") }));
    expect(submit).toHaveBeenCalledTimes(2);
  }
});

it("retains the query through an install preview and return", async () => {
  const { user, search, plan, install } = await setup();
  const shown = await new MockPort().planOwnMarket({ slug: "demo/ship-notes" });
  plan.mockResolvedValue(shown);
  await user.type(search, "review");
  await user.click(screen.getByRole("button", { name: t("安装") }));
  await screen.findByRole("button", { name: t("返回") });
  expect(plan).toHaveBeenCalledExactlyOnceWith({ slug: review.slug, replace: false });
  expect(install).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: t("返回") }));
  expect(screen.getByRole<HTMLInputElement>("searchbox").value).toBe("review");
  expect(screen.getAllByRole("listitem")).toHaveLength(1);
});

it("passes the canonical package to the new-version action after filtering", async () => {
  const { user, search, props } = await setup();
  await user.type(search, "café");
  await user.click(screen.getByRole("button", { name: t("发布新版本") }));
  expect(props.onPublish).toHaveBeenCalledExactlyOnceWith(review);
});

it.each(["success", "failure"])("resets query on a connection switch and ignores the old read %s", async (outcome) => {
  const { user, search, read, view, props } = await setup();
  await user.type(search, "review");
  const next = new MockPort() as unknown as AgentPort;
  const request = pending<MarketPackage[]>();
  vi.spyOn(next, "myMarket").mockImplementationOnce(() => request.promise);
  view.rerender(<MyPackages {...props} port={next} />);
  const old = pending<MarketPackage[]>();
  read.mockImplementationOnce(() => old.promise);
  view.rerender(<MyPackages {...props} />);
  expect(screen.getByRole<HTMLInputElement>("searchbox").value).toBe("");
  expect(screen.queryByText(t("没有找到匹配的包。"))).toBeNull();
  await act(async () => old.resolve(rows));
  await act(async () => outcome === "success" ? request.resolve([]) : request.reject(new Error("previous list failed")));
  expect(screen.getAllByRole("listitem")).toHaveLength(2);
  expect(screen.queryByText("previous list failed")).toBeNull();
});

it("keeps a list-read failure distinct from a search miss and retains the query on retry", async () => {
  const { view, props, user } = await setup();
  const next = new MockPort() as unknown as AgentPort;
  const read = vi.spyOn(next, "myMarket").mockRejectedValueOnce(new Error("list unavailable"));
  view.rerender(<MyPackages {...props} port={next} />);
  await screen.findByText("list unavailable");
  await user.type(screen.getByRole("searchbox"), "review");
  expect(screen.queryByText(t("没有找到匹配的包。"))).toBeNull();
  read.mockResolvedValueOnce(rows);
  await user.click(screen.getByRole("button", { name: t("重试") }));
  await screen.findByText(review.name);
  expect(screen.getByRole<HTMLInputElement>("searchbox").value).toBe("review");
  expect(screen.getAllByRole("listitem")).toHaveLength(1);
});

it("keeps a genuinely empty account distinct from an unmatched query", async () => {
  const { view, props, user } = await setup();
  const next = new MockPort() as unknown as AgentPort;
  vi.spyOn(next, "myMarket").mockResolvedValue([]);
  view.rerender(<MyPackages {...props} port={next} />);
  await screen.findByText(t("还没有发布过。"));
  await user.type(screen.getByRole("searchbox"), "review");
  expect(screen.getByText(t("还没有发布过。"))).toBeTruthy();
  expect(screen.queryByText(t("没有找到匹配的包。"))).toBeNull();
});
