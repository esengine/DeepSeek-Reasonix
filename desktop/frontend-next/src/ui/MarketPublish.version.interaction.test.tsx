// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { boot, STORAGE, t } from "../i18n";
import { MarketGroup } from "./Market";
import { MockPort } from "../port/mock";
import type { AgentPort, MarketKind, MarketPackage, MarketPublished } from "../port/port";

afterEach(() => { cleanup(); localStorage.setItem(STORAGE, "zh"); boot(); });

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

const source = "https://github.com/demo/releases/tree/" + "b".repeat(40) + "/release-kit";
const field = (key: string) => screen.getByLabelText<HTMLInputElement | HTMLTextAreaElement>(new RegExp(`^${t(key === "标签" && screen.queryByText(t("新增标签")) ? "新增标签" : key)}`));
const existingTags = () => screen.getAllByRole<HTMLInputElement>("textbox", { name: new RegExp(`^${t("已有标签 {n}", { n: "" })}`) });
const draw = (port: AgentPort, handle = "demo") => <MarketGroup port={port} account={{ signedIn: true, user: { handle, email: `${handle}@example.com`, label: handle } }} onInstalled={() => {}} onSignIn={() => {}} />;

async function fixture(kind: MarketKind = "plugin", status = "private", latestVersion = "1.4.9") {
  const port = new MockPort() as unknown as AgentPort;
  const base = (await port.myMarket())[0]!;
  const pkg: MarketPackage = {
    ...base, kind, status, name: "release-kit", handle: "demo", slug: "demo/release-kit", latestVersion,
    summary: "Existing summary", description: "Existing description\nwith a second line.",
    repoUrl: "https://github.com/demo/releases", tags: ["ui,ux", "文档，示例", "tools"], installed: undefined,
  };
  const read = vi.spyOn(port, "myMarket").mockResolvedValue([pkg]);
  const receipt: MarketPublished = { created: false, version: "1.4.10", package: { ...pkg, latestVersion: "1.4.10" } };
  const publish = vi.spyOn(port, "publishMarket").mockResolvedValue(receipt);
  const preview = vi.spyOn(port, "planOwnMarket");
  const install = vi.spyOn(port, "installOwnMarket");
  const review = vi.spyOn(port, "submitMarket");
  const view = render(draw(port));
  await userEvent.click(screen.getByRole("radio", { name: t("我的发布") }));
  const row = (await screen.findByText(pkg.name)).closest("li")!;
  return { port, pkg, read, receipt, publish, preview, install, review, row, view };
}

const openDraft = async (row: HTMLElement) => userEvent.click(within(row).getByRole("button", { name: t("发布新版本") }));
const fillSource = () => fireEvent.change(field("来源地址"), { target: { value: source } });

const nameHint = {
  zh: "要发布这个包的新版本，请保持名称不变；改名会发布为另一个包。",
  en: "Keep this name to publish a new version of this package. Changing it publishes a different package.",
};

it.each(["zh", "en"].flatMap((lang) => ["1.4.9-rc.1", "release-2026"].map((latest) => ({ lang, latest }))))(
  "explains blank-version limits and preserves a corrected $latest update in $lang", async ({ lang, latest }) => {
    localStorage.setItem(STORAGE, lang); boot();
    const f = await fixture("plugin", "active", latest);
    f.publish.mockRejectedValueOnce(new Error("That version is already published"));
    await openDraft(f.row);
    const version = field("版本");
    const hint = within(version.closest("label")!).getByText(lang === "zh"
      ? "留空时新包为 0.1.0；更新只自动递增纯数字三段版本的补丁号，其他版本请明确填写。"
      : "Blank starts a new package at 0.1.0. Updates auto-increment only a numeric three-part version; enter other versions explicitly.");
    expect(version.value).toBe("");
    fillSource();
    await userEvent.click(screen.getByRole("button", { name: t("提交审核") }));
    await screen.findByText("That version is already published");
    expect(f.publish).toHaveBeenLastCalledWith(expect.objectContaining({ version: "", source }));
    fireEvent.change(version, { target: { value: `${latest}.next` } });
    await userEvent.click(screen.getByRole("button", { name: t("提交审核") }));
    expect(f.publish).toHaveBeenLastCalledWith({
      kind: f.pkg.kind, name: f.pkg.name, source, summary: f.pkg.summary, description: f.pkg.description,
      repoUrl: f.pkg.repoUrl, version: `${latest}.next`, tags: f.pkg.tags, visibility: "public",
    });
    expect(hint).toBeTruthy();
  },
);

it.each(["zh", "en"] as const)("explains the name choice and submits the author's edited name in %s", async (lang) => {
  localStorage.setItem(STORAGE, lang); boot();
  const f = await fixture();
  await openDraft(f.row);
  const name = field("名称");
  expect(within(name.closest("label")!).getByText(nameHint[lang])).toBeTruthy();
  expect(name.disabled).toBe(false);
  await userEvent.clear(name);
  await userEvent.type(name, "another-kit");
  fillSource();
  expect(within(name.closest("label")!).getByText(nameHint[lang])).toBeTruthy();
  await userEvent.click(screen.getByRole("button", { name: t("保存为私有") }));
  expect(f.publish).toHaveBeenCalledWith(expect.objectContaining({ name: "another-kit", version: "", source, visibility: "private" }));
});

it.each([
  ["skill", "private", "zh"], ["plugin", "pending", "zh"], ["mcp", "rejected", "zh"], ["theme", "active", "zh"],
  ["skill", "private", "en"], ["plugin", "pending", "en"], ["mcp", "rejected", "en"], ["theme", "active", "en"],
] as const)("opens an editable %s/%s release draft in %s without publishing or installing", async (kind, status, lang) => {
  localStorage.setItem(STORAGE, lang); boot();
  const f = await fixture(kind, status);
  const action = within(f.row).getByRole("button", { name: t("发布新版本") });
  action.focus();
  await userEvent.keyboard("{Enter}");
  expect(screen.getByRole("radio", { name: t("发布") }).getAttribute("aria-checked")).toBe("true");
  expect(field("名称").value).toBe(f.pkg.name);
  expect(field("摘要").value).toBe(f.pkg.summary);
  expect(field("描述").value).toBe(f.pkg.description);
  expect(field("仓库").value).toBe(f.pkg.repoUrl);
  expect(existingTags().map((input) => input.value)).toEqual(f.pkg.tags);
  expect(field("标签").value).toBe("");
  expect(field("版本").value).toBe("");
  expect(field("来源地址").value).toBe("");
  expect(document.activeElement).toBe(field("来源地址"));
  expect(screen.getByRole<HTMLInputElement>("checkbox").checked).toBe(status === "private");
  expect(screen.getByText(t("从 {slug} 复用发布资料；请填写本次发布的来源地址。", { slug: f.pkg.slug }))).toBeTruthy();
  expect(screen.getByText(t(status === "private"
    ? "以 @{handle} 的名义保存，仅自己可见，不提交审核。只收来源地址，不上传文件。"
    : "以 @{handle} 的名义提交，审核通过后公开。只收来源地址，不上传文件。", { handle: "demo" }))).toBeTruthy();
  const names = { skill: "技能", plugin: "插件", mcp: "MCP 服务", theme: "主题" };
  expect(screen.getByRole("radio", { name: t(names[kind]) }).getAttribute("aria-checked")).toBe("true");
  expect(screen.getByRole<HTMLButtonElement>("button", { name: t(status === "private" ? "保存为私有" : "提交审核") }).disabled).toBe(true);
  expect(f.publish).not.toHaveBeenCalled();
  expect(f.preview).not.toHaveBeenCalled();
  expect(f.install).not.toHaveBeenCalled();
  expect(f.review).not.toHaveBeenCalled();
});

it.each(["private", "active"])("submits the confirmed %s draft with the new source and unchanged tag identities", async (status) => {
  const f = await fixture("plugin", status);
  await openDraft(f.row);
  fillSource();
  await userEvent.click(screen.getByRole("button", { name: status === "private" ? "保存为私有" : "提交审核" }));
  expect(f.publish).toHaveBeenCalledTimes(1);
  expect(f.publish).toHaveBeenCalledWith({
    kind: f.pkg.kind, name: f.pkg.name, source, summary: f.pkg.summary, description: f.pkg.description,
    repoUrl: f.pkg.repoUrl, version: "", tags: f.pkg.tags, visibility: status === "private" ? "private" : "public",
  });
  await userEvent.click(await screen.findByRole("button", { name: "查看我的发布" }));
  expect(f.read).toHaveBeenCalledTimes(2);
  expect(f.preview).not.toHaveBeenCalled();
  expect(f.install).not.toHaveBeenCalled();
});

it("submits explicit edits without retaining the old version or tag list", async () => {
  const f = await fixture();
  await openDraft(f.row);
  fillSource();
  fireEvent.change(field("版本"), { target: { value: "2.0.0" } });
  fireEvent.change(field("摘要"), { target: { value: "Updated summary" } });
  for (const input of existingTags()) fireEvent.change(input, { target: { value: "" } });
  fireEvent.change(field("标签"), { target: { value: "release， docs, " } });
  await userEvent.click(screen.getByRole("checkbox"));
  await userEvent.click(screen.getByRole("button", { name: "提交审核" }));
  expect(f.publish).toHaveBeenCalledWith(expect.objectContaining({ version: "2.0.0", summary: "Updated summary", tags: ["release", "docs"], visibility: "public" }));
});

it("preserves the release draft through a failed submission and allows a corrected retry", async () => {
  const f = await fixture();
  const pending = deferred<MarketPublished>();
  f.publish.mockImplementationOnce(() => pending.promise);
  await openDraft(f.row);
  fillSource();
  await userEvent.click(screen.getByRole("button", { name: "保存为私有" }));
  expect(field("来源地址").disabled).toBe(true);
  expect(screen.getByRole<HTMLInputElement>("checkbox").disabled).toBe(true);
  await act(async () => pending.reject(new Error("release refused")));
  await screen.findByText("release refused");
  expect(field("来源地址").value).toBe(source);
  expect(field("来源地址").disabled).toBe(false);
  expect(field("名称").value).toBe(f.pkg.name);
  expect(existingTags().map((input) => input.value)).toEqual(f.pkg.tags);
  expect(field("标签").value).toBe("");
  expect(screen.getByRole<HTMLInputElement>("checkbox").checked).toBe(true);
  fireEvent.change(field("版本"), { target: { value: "1.4.11" } });
  await userEvent.click(screen.getByRole("button", { name: "保存为私有" }));
  expect(f.publish).toHaveBeenLastCalledWith(expect.objectContaining({ source, version: "1.4.11", tags: f.pkg.tags, visibility: "private" }));
});

it.each(["tab", "another"])("starts a clean ordinary draft through %s after preparing a release", async (path) => {
  const f = await fixture();
  await openDraft(f.row);
  fillSource();
  if (path === "tab") {
    await userEvent.click(screen.getByRole("radio", { name: "我的发布" }));
    await screen.findByText(f.pkg.name);
    await userEvent.click(screen.getByRole("radio", { name: "发布" }));
  } else {
    await userEvent.click(screen.getByRole("button", { name: "保存为私有" }));
    await userEvent.click(await screen.findByRole("button", { name: "再发布一个" }));
  }
  for (const key of ["名称", "版本", "来源地址", "摘要", "描述", "仓库", "标签"]) expect(field(key).value).toBe("");
  expect(screen.getByRole<HTMLInputElement>("checkbox").checked).toBe(false);
  expect(screen.queryByText(/demo\/release-kit.*来源地址/)).toBeNull();
  expect(screen.queryByText(nameHint.zh)).toBeNull();
  expect(screen.getByRole<HTMLButtonElement>("button", { name: "提交审核" }).disabled).toBe(true);
});

it.each(["connection", "account"])("clears the selected release and edits across a %s change, including a return", async (owner) => {
  const f = await fixture();
  await openDraft(f.row);
  fillSource();
  const next = owner === "connection" ? new MockPort() as unknown as AgentPort : f.port;
  f.view.rerender(draw(next, owner === "account" ? "other" : "demo"));
  for (const key of ["名称", "来源地址", "摘要", "描述", "仓库", "标签"]) expect(field(key).value).toBe("");
  expect(screen.getByRole<HTMLInputElement>("checkbox").checked).toBe(false);
  f.view.rerender(draw(f.port));
  expect(field("名称").value).toBe("");
  expect(field("来源地址").value).toBe("");
  expect(f.publish).not.toHaveBeenCalled();
});

it.each(["success", "failure"])("reads the replacement account's release list while ignoring an old list %s", async (outcome) => {
  const f = await fixture();
  await openDraft(f.row);
  const old = deferred<MarketPackage[]>();
  f.read.mockImplementationOnce(() => old.promise);
  await userEvent.click(screen.getByRole("radio", { name: "我的发布" }));
  const pkg = { ...f.pkg, handle: "other", name: "other-kit", slug: "other/other-kit" };
  f.read.mockResolvedValueOnce([pkg]);
  f.view.rerender(draw(f.port, "other"));
  await screen.findByText("other-kit");
  await act(async () => {
    if (outcome === "success") old.resolve([f.pkg]);
    else old.reject(new Error("old list unavailable"));
  });
  expect(screen.queryByText(f.pkg.name)).toBeNull();
  expect(screen.queryByText("old list unavailable")).toBeNull();
  await openDraft(screen.getByText("other-kit").closest("li")!);
  expect(field("名称").value).toBe("other-kit");
  expect(screen.getByText(t("以 @{handle} 的名义保存，仅自己可见，不提交审核。只收来源地址，不上传文件。", { handle: "other" }))).toBeTruthy();
});

it.each(["zh", "en"])("keeps prefilled comma-containing tags when adding a tag in %s", async (lang) => {
  localStorage.setItem(STORAGE, lang); boot();
  const f = await fixture();
  await openDraft(f.row);
  fillSource();
  await userEvent.type(field("标签"), ", release");
  await userEvent.click(screen.getByRole("button", { name: t("保存为私有") }));
  expect(f.publish).toHaveBeenCalledWith(expect.objectContaining({ tags: [...f.pkg.tags, "release"] }));
});

it.each(["zh", "en"])("edits an existing tag as one item and retains it through a failed submission in %s", async (lang) => {
  localStorage.setItem(STORAGE, lang); boot();
  const f = await fixture();
  f.publish.mockRejectedValueOnce(new Error("registry unavailable"));
  await openDraft(f.row);
  fillSource();
  const input = existingTags()[0]!;
  input.focus();
  await userEvent.keyboard("{End},docs");
  await userEvent.type(field("标签"), "new， extra");
  const tags = ["ui,ux,docs", "文档，示例", "tools", "new", "extra"];
  await userEvent.click(screen.getByRole("button", { name: t("保存为私有") }));
  await screen.findByText("registry unavailable");
  expect(existingTags().map((tag) => tag.value)).toEqual(tags.slice(0, 3));
  expect(field("标签").value).toBe("new， extra");
  await userEvent.click(screen.getByRole("button", { name: t("保存为私有") }));
  expect(f.publish).toHaveBeenNthCalledWith(1, expect.objectContaining({ tags }));
  expect(f.publish).toHaveBeenNthCalledWith(2, expect.objectContaining({ tags }));
});
