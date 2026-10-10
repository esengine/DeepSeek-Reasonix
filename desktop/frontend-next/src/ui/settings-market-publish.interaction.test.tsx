// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { StrictMode } from "react";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Settings } from "./Settings";
import { MarketGroup } from "./Market";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";
import { boot, STORAGE, t } from "../i18n";
import type { AgentPort, MarketPlan, MarketPublished, SessionStatus } from "../port/port";

afterEach(() => { cleanup(); vi.restoreAllMocks(); localStorage.setItem(STORAGE, "zh"); boot(); });

const account = (handle = "demo") => ({ signedIn: true, user: { handle, email: `${handle}@example.com`, label: handle } });
const field = (label: string) => screen.getByLabelText<HTMLInputElement>(new RegExp(`^${t(label)}`));
const published = () => document.querySelector<HTMLElement>('.mkt-pub[data-stage="done"] [role="status"]');
const fill = (name = "notes-kit") => {
  fireEvent.change(field("名称"), { target: { value: name } });
  fireEvent.change(field("来源地址"), { target: { value: `https://example.com/${name}/SKILL.md` } });
};
function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
const receipt = async (port: AgentPort, privateDraft = false, name = "notes-kit") => port.publishMarket({
  kind: "skill", name, source: `https://example.com/${name}/SKILL.md`, visibility: privateDraft ? "private" : "public",
});
const draw = (port: AgentPort, onClose = vi.fn()) => <Settings
  hub={new MockHub() as never} port={port}
  status={{ preset: "balanced", toolApprovalMode: "ask" } as SessionStatus}
  theme="light" onTheme={() => {}} contrast="" onContrast={() => {}} weight="" onWeight={() => {}}
  look={{} as never} onLook={() => {}} reloadThemes={() => {}}
  onClose={onClose} onChanged={() => {}} onError={() => {}} at="ext:market"
  account={account()} accountUnread="" reloadAccount={() => {}}
/>;

it("keeps a pending publish receipt visible after clicking the browse view", async () => {
  const port = new MockPort() as unknown as AgentPort;
  const out = await receipt(port);
  const pending = deferred<MarketPublished>();
  vi.spyOn(port, "publishMarket").mockImplementation(() => pending.promise);
  render(draw(port));
  await userEvent.click(screen.getByRole("radio", { name: "发布" }));
  fill();
  await userEvent.click(screen.getByRole("button", { name: "提交审核" }));
  await userEvent.click(screen.getByRole("radio", { name: "浏览" }));
  await act(async () => pending.resolve(out));
  expect(published()).not.toBeNull();
  expect(published()?.textContent).toContain("已提交 demo/notes-kit 0.1.0");
});

it.each(["zh", "en"].flatMap((lang) => [false, true].flatMap((privateDraft) =>
  ["success", "failure"].map((outcome) => ({ lang, privateDraft, outcome })),
)))("holds the $lang author form and Settings navigation through private=$privateDraft publish $outcome", async ({ lang, privateDraft, outcome }) => {
  localStorage.setItem(STORAGE, lang); boot();
  const port = new MockPort() as unknown as AgentPort;
  const out = await receipt(port, privateDraft);
  const pending = deferred<MarketPublished>();
  const publish = vi.spyOn(port, "publishMarket").mockImplementationOnce(() => pending.promise).mockResolvedValue(out);
  const onClose = vi.fn();
  render(draw(port, onClose));
  await userEvent.click(screen.getByRole("radio", { name: t("发布") }));
  fill();
  if (privateDraft) await userEvent.click(screen.getByRole("checkbox"));
  await userEvent.click(screen.getByRole("button", { name: t(privateDraft ? "保存为私有" : "提交审核") }));
  const form = document.querySelector<HTMLFormElement>("form.mkt-pub")!;
  const controls = [
    document.querySelector<HTMLButtonElement>('[data-action="settings.close"]')!,
    document.querySelector<HTMLButtonElement>('[data-action="settings.section"][data-value="session"]')!,
    screen.getByRole<HTMLButtonElement>("tab", { name: t("已安装") }),
    screen.getByRole<HTMLButtonElement>("radio", { name: t("浏览") }),
    screen.getByRole<HTMLButtonElement>("radio", { name: t("我的发布") }),
  ];
  for (const button of controls) {
    expect(button.disabled).toBe(true);
    await userEvent.click(button);
  }
  await userEvent.type(screen.getByRole("textbox", { name: t("搜索设置") }), "模型");
  const found = screen.getAllByRole<HTMLButtonElement>("option");
  for (const button of found) {
    expect(button.disabled).toBe(true);
    await userEvent.click(button);
  }
  await userEvent.click(form);
  await userEvent.keyboard("{Escape}");
  const backdrop = document.querySelector<HTMLElement>(".prefs")!;
  fireEvent.mouseDown(backdrop);
  fireEvent.mouseUp(backdrop);
  expect(onClose).not.toHaveBeenCalled();
  expect(form.isConnected).toBe(true);
  expect(field("名称").value).toBe("notes-kit");
  expect(form.getAttribute("aria-busy")).toBe("true");
  expect(publish).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ name: "notes-kit", visibility: privateDraft ? "private" : "public" }));
  await act(async () => {
    if (outcome === "success") pending.resolve(out);
    else pending.reject(new Error("registry temporarily unavailable"));
  });
  for (const button of [...controls, ...found]) expect(button.disabled).toBe(false);
  if (outcome === "success") {
    expect(published()?.textContent).toContain(out.package.slug);
    expect(screen.queryByRole("alert")).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: t("再发布一个") }));
    expect(field("名称").value).toBe("");
    expect(field("来源地址").value).toBe("");
  } else {
    expect(screen.getByRole("alert").textContent).toContain("registry temporarily unavailable");
    expect(field("名称").value).toBe("notes-kit");
    expect(field("来源地址").value).toBe("https://example.com/notes-kit/SKILL.md");
    await userEvent.click(screen.getByRole("button", { name: t(privateDraft ? "保存为私有" : "提交审核") }));
    expect(published()?.textContent).toContain(out.package.slug);
    expect(publish).toHaveBeenCalledTimes(2);
    expect(screen.queryByRole("alert")).toBeNull();
  }
  await userEvent.click(controls[0]!);
  expect(onClose).toHaveBeenCalledTimes(1);
});

it.each(["connection", "account"].flatMap((owner) => ["success", "failure"].map((outcome) => ({ owner, outcome }))))(
  "releases a changed $owner but does not let its late $outcome unlock a new submission",
  async ({ owner, outcome }) => {
    const port = new MockPort() as unknown as AgentPort;
    const next = new MockPort() as unknown as AgentPort;
    const out = await receipt(port);
    const old = deferred<MarketPublished>();
    const fresh = deferred<MarketPublished>();
    vi.spyOn(port, "publishMarket").mockImplementationOnce(() => old.promise).mockImplementationOnce(() => fresh.promise);
    vi.spyOn(next, "publishMarket").mockImplementation(() => fresh.promise);
    const applying = vi.fn();
    const group = (p: AgentPort, handle = "demo") => <MarketGroup port={p} account={account(handle)} onInstalled={() => {}} onSignIn={() => {}} onApplying={applying} />;
    const view = render(group(port));
    await userEvent.click(screen.getByRole("radio", { name: "发布" }));
    fill();
    await userEvent.click(screen.getByRole("button", { name: "提交审核" }));
    expect(applying).toHaveBeenLastCalledWith(true);
    view.rerender(group(owner === "connection" ? next : port, owner === "account" ? "other" : "demo"));
    expect(applying).toHaveBeenLastCalledWith(false);
    expect(screen.getByRole<HTMLButtonElement>("radio", { name: "浏览" }).disabled).toBe(false);
    fill("fresh-kit");
    await userEvent.click(screen.getByRole("button", { name: "提交审核" }));
    expect(applying).toHaveBeenLastCalledWith(true);
    const calls = applying.mock.calls.length;
    await act(async () => {
      if (outcome === "success") old.resolve(out);
      else old.reject(new Error("old publish refused"));
    });
    expect(applying).toHaveBeenCalledTimes(calls);
    expect(screen.getByRole<HTMLButtonElement>("radio", { name: "浏览" }).disabled).toBe(true);
    expect(screen.queryByRole("status")).toBeNull();
    expect(screen.queryByRole("alert")).toBeNull();
    await act(async () => fresh.resolve({ ...out, package: { ...out.package, name: "fresh-kit", slug: "demo/fresh-kit" } }));
    expect(applying).toHaveBeenLastCalledWith(false);
    expect(screen.getByRole<HTMLButtonElement>("radio", { name: "浏览" }).disabled).toBe(false);
    expect(published()?.textContent).toContain("demo/fresh-kit");
  },
);

it("releases the navigation channel on unmount during a StrictMode submission", async () => {
  const port = new MockPort() as unknown as AgentPort;
  const out = await receipt(port);
  const pending = deferred<MarketPublished>();
  vi.spyOn(port, "publishMarket").mockImplementation(() => pending.promise);
  const applying = vi.fn();
  const view = render(<StrictMode><MarketGroup port={port} account={account()} onInstalled={() => {}} onSignIn={() => {}} onApplying={applying} /></StrictMode>);
  await userEvent.click(screen.getByRole("radio", { name: "发布" }));
  fill();
  await userEvent.click(screen.getByRole("button", { name: "提交审核" }));
  expect(applying).toHaveBeenLastCalledWith(true);
  view.unmount();
  expect(applying).toHaveBeenLastCalledWith(false);
  const calls = applying.mock.calls.length;
  await act(async () => pending.resolve(out));
  expect(applying).toHaveBeenCalledTimes(calls);
});

it.each(["publish", "install"].flatMap((first) => ["success", "failure"].flatMap((outcome) =>
  [false, true].map((strict) => ({ first, outcome, strict })),
)))("keeps views exclusive and the replacement hold during old $first $outcome (StrictMode=$strict)", async ({ first, outcome, strict }) => {
  const old = new MockPort() as unknown as AgentPort;
  const current = new MockPort() as unknown as AgentPort;
  const pkg = (await old.myMarket()).find((p) => p.slug === "demo/ship-notes")!;
  const shown = await old.planOwnMarket({ slug: pkg.slug });
  const installed: MarketPlan = { ...shown, applied: true, status: "done" };
  const out = await receipt(old);
  const oldPublish = deferred<MarketPublished>();
  const newPublish = deferred<MarketPublished>();
  const oldInstall = deferred<MarketPlan>();
  const newInstall = deferred<MarketPlan>();
  const publishOld = vi.spyOn(old, "publishMarket").mockReturnValue(oldPublish.promise);
  const publishNew = vi.spyOn(current, "publishMarket").mockReturnValue(newPublish.promise);
  const installOld = vi.spyOn(old, "installOwnMarket").mockReturnValue(oldInstall.promise);
  const installNew = vi.spyOn(current, "installOwnMarket").mockReturnValue(newInstall.promise);
  for (const port of [old, current]) {
    vi.spyOn(port, "myMarket").mockResolvedValue([pkg]);
    vi.spyOn(port, "planOwnMarket").mockResolvedValue(shown);
  }
  const applying = vi.fn();
  const onInstalled = vi.fn();
  const group = (port: AgentPort) => {
    const child = <MarketGroup port={port} account={account()} onInstalled={onInstalled} onSignIn={() => {}} onApplying={applying} />;
    return strict ? <StrictMode>{child}</StrictMode> : child;
  };
  const begin = async (kind: string) => {
    if (kind === "publish") {
      await userEvent.click(screen.getByRole("radio", { name: "发布" }));
      fill();
      await userEvent.click(screen.getByRole("button", { name: "提交审核" }));
      expect(document.querySelector("form.mkt-pub")).not.toBeNull();
      expect(screen.queryByRole("button", { name: "安装中…" })).toBeNull();
    } else {
      await userEvent.click(screen.getByRole("radio", { name: "我的发布" }));
      const row = (await screen.findByText(pkg.name)).closest("li")!;
      await userEvent.click(row.querySelector<HTMLButtonElement>('[data-action="market.own-inspect"]')!);
      await userEvent.click(await screen.findByRole("button", { name: "安装" }));
      expect(screen.getByRole<HTMLButtonElement>("button", { name: "安装中…" }).disabled).toBe(true);
      expect(document.querySelector("form.mkt-pub")).toBeNull();
    }
  };
  const view = render(group(old));
  await begin(first);
  expect(applying).toHaveBeenLastCalledWith(true);
  for (const radio of screen.getAllByRole<HTMLButtonElement>("radio")) {
    expect(radio.disabled).toBe(true);
    await userEvent.click(radio);
  }
  expect(first === "publish" ? publishOld : installOld).toHaveBeenCalledTimes(1);
  expect(first === "publish" ? installOld : publishOld).not.toHaveBeenCalled();
  view.rerender(group(current));
  expect(applying).toHaveBeenLastCalledWith(false);
  await begin(first === "publish" ? "install" : "publish");
  expect(applying).toHaveBeenLastCalledWith(true);
  expect(first === "publish" ? installNew : publishNew).toHaveBeenCalledTimes(1);
  const calls = applying.mock.calls.length;
  await act(async () => {
    if (first === "publish") {
      if (outcome === "success") oldPublish.resolve(out);
      else oldPublish.reject(new Error("old publish refused"));
    } else {
      if (outcome === "success") oldInstall.resolve(installed);
      else oldInstall.reject(new Error("old install refused"));
    }
  });
  expect(applying).toHaveBeenCalledTimes(calls);
  expect(onInstalled).not.toHaveBeenCalled();
  expect(screen.queryByRole("alert")).toBeNull();
  for (const radio of screen.getAllByRole<HTMLButtonElement>("radio")) expect(radio.disabled).toBe(true);
  await act(async () => first === "publish" ? newInstall.resolve(installed) : newPublish.resolve(out));
  expect(applying).toHaveBeenLastCalledWith(false);
  for (const radio of screen.getAllByRole<HTMLButtonElement>("radio")) expect(radio.disabled).toBe(false);
  expect(onInstalled).toHaveBeenCalledTimes(first === "publish" ? 1 : 0);
});
