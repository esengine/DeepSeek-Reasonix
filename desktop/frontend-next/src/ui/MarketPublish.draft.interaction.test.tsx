// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { PublishForm } from "./MarketPublish";
import { MockPort } from "../port/mock";
import { boot, STORAGE, t } from "../i18n";
import type { AgentPort, MarketPublished, MarketSubmission } from "../port/port";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  localStorage.setItem(STORAGE, "zh");
  boot();
});

const values = {
  名称: "notes-kit", 版本: "0.3.0", 来源地址: "https://github.com/demo/notes-kit",
  摘要: "Review notes", 描述: "A team checklist", 仓库: "https://github.com/demo/notes-kit", 标签: "notes，team",
};
const field = (name: string) => screen.getByLabelText<HTMLInputElement | HTMLTextAreaElement>(new RegExp(`^${t(name)}`));
const request: MarketSubmission = {
  kind: "skill", name: values.名称, version: values.版本, source: values.来源地址,
  summary: values.摘要, description: values.描述, repoUrl: values.仓库, tags: ["notes", "team"],
};

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

it.each(["zh", "en"].flatMap((lang) => ["public", "private"].flatMap((visibility) =>
  ["success", "failure"].map((outcome) => ({ lang, visibility, outcome })),
)))("keeps the submitted $visibility draft during a $lang $outcome", async ({ lang, visibility, outcome }) => {
  localStorage.setItem(STORAGE, lang);
  boot();
  const port = new MockPort() as unknown as AgentPort;
  const sent = { ...request, visibility: visibility as "public" | "private" };
  const receipt = await port.publishMarket(sent);
  const pending = deferred<MarketPublished>();
  const publish = vi.spyOn(port, "publishMarket").mockImplementationOnce(() => pending.promise).mockResolvedValue(receipt);
  render(<PublishForm port={port} handle="demo" onMine={() => {}} />);
  const publicSummary = lang === "zh"
    ? "以 @demo 的名义提交，审核通过后公开。只收来源地址，不上传文件。"
    : "Submitted as @demo and made public once approved. Only the source address is sent; no files are uploaded.";
  const privateSummary = lang === "zh"
    ? "以 @demo 的名义保存，仅自己可见，不提交审核。只收来源地址，不上传文件。"
    : "Saved as @demo, visible only to you and not submitted for review. Only the source address is sent; no files are uploaded.";
  expect(screen.getByText(publicSummary)).toBeTruthy();
  for (const [name, value] of Object.entries(values)) fireEvent.change(field(name), { target: { value } });
  if (visibility === "private") await userEvent.click(screen.getByRole("checkbox"));
  expect(screen.getByText(visibility === "private" ? privateSummary : publicSummary)).toBeTruthy();
  await userEvent.click(screen.getByRole("button", { name: t(visibility === "private" ? "保存为私有" : "提交审核") }));

  for (const name of Object.keys(values)) await userEvent.type(field(name), "edited");
  await userEvent.click(screen.getByRole("radio", { name: t("主题") }));
  await userEvent.click(screen.getByRole("checkbox"));
  await userEvent.click(screen.getByRole("button", { name: t("提交中…") }));
  const draft = Object.fromEntries(Object.keys(values).map((name) => [name, field(name).value]));
  const kind = screen.getByRole("radio", { name: t("技能") }).getAttribute("aria-checked");
  const privateDraft = screen.getByRole<HTMLInputElement>("checkbox").checked;
  const busy = document.querySelector(".mkt-pub")!.getAttribute("aria-busy");
  expect(screen.getByText(visibility === "private" ? privateSummary : publicSummary)).toBeTruthy();
  await act(async () => {
    if (outcome === "success") pending.resolve(receipt);
    else pending.reject(new Error("registry temporarily unavailable"));
  });

  expect(draft).toEqual(values);
  expect(kind).toBe("true");
  expect(privateDraft).toBe(visibility === "private");
  expect(busy).toBe("true");
  expect(publish).toHaveBeenCalledExactlyOnceWith(sent);
  if (outcome === "failure") {
    expect(screen.getByRole("alert").textContent).toContain("registry temporarily unavailable");
    expect(screen.queryByRole("status")).toBeNull();
    expect(document.querySelector(".mkt-pub")!.getAttribute("aria-busy")).toBe("false");
    await userEvent.clear(field("来源地址"));
    await userEvent.type(field("来源地址"), "https://github.com/demo/fixed-kit");
    await userEvent.click(screen.getByRole("radio", { name: t("主题") }));
    await userEvent.click(screen.getByRole("checkbox"));
    expect(screen.getByText(visibility === "private" ? publicSummary : privateSummary)).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: t(visibility === "private" ? "提交审核" : "保存为私有") }));
    expect(publish).toHaveBeenCalledTimes(2);
    expect(publish).toHaveBeenLastCalledWith({
      ...sent, kind: "theme", source: "https://github.com/demo/fixed-kit", visibility: visibility === "private" ? "public" : "private",
    });
    expect((await screen.findByRole("status")).textContent).toContain(receipt.package.slug);
    expect(screen.queryByRole("alert")).toBeNull();
  } else {
    expect(screen.getByRole("status").textContent).toContain(t(visibility === "private" ? "已保存 {slug} {version}，仅自己可见" : "已提交 {slug} {version}", {
      slug: receipt.package.slug, version: receipt.version,
    }));
    expect(screen.queryByRole("alert")).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: t("再发布一个") }));
    for (const name of Object.keys(values)) expect(field(name).value).toBe("");
    expect(screen.getByRole<HTMLInputElement>("checkbox").checked).toBe(false);
    expect(screen.getByRole("radio", { name: t("技能") }).getAttribute("aria-checked")).toBe("true");
    expect(screen.getByText(publicSummary)).toBeTruthy();
    expect(screen.queryByRole("status")).toBeNull();
    expect(screen.queryByRole("alert")).toBeNull();
  }
});
