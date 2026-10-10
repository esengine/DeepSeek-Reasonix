// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { boot, STORAGE, t } from "../i18n";
import { MockPort } from "../port/mock";
import type { AgentPort, MarketPackage, MarketPublished } from "../port/port";
import { PublishForm } from "./MarketPublish";

afterEach(() => { cleanup(); localStorage.setItem(STORAGE, "zh"); boot(); });

const source = "https://github.com/demo/themes/tree/" + "a".repeat(40) + "/dusk";
const field = (name: string) => document.querySelector<HTMLInputElement>(`input[data-value="${name}"]`)!;

function setup(lang: string, initial?: MarketPackage) {
  localStorage.setItem(STORAGE, lang);
  boot();
  const port = new MockPort() as unknown as AgentPort;
  const onMine = vi.fn();
  render(<PublishForm port={port} handle="demo" onMine={onMine} initial={initial} />);
  return { port, onMine, user: userEvent.setup() };
}

it.each(["zh", "en"].flatMap((lang) => ["public", "private"].map((visibility) => ({ lang, visibility }))))(
  "submits once with Enter and retains the pending lock ($lang/$visibility)",
  async ({ lang, visibility }) => {
    const { port, onMine, user } = setup(lang);
    const original = port.publishMarket.bind(port);
    let finish!: () => Promise<void>;
    const publish = vi.spyOn(port, "publishMarket").mockImplementation((submission) => new Promise<MarketPublished>((resolve) => {
      finish = async () => resolve(await original(submission));
    }));
    await user.click(screen.getByRole("radio", { name: t("主题") }));
    await user.type(field("name"), "enter-kit");
    await user.type(field("version"), "0.2.0");
    await user.type(field("summary"), "Keyboard release");
    await user.type(field("tags"), "theme，keyboard");
    if (visibility === "private") await user.click(screen.getByRole("checkbox"));
    await user.type(field("source"), source);
    await user.keyboard("{Enter}");
    await waitFor(() => expect(publish).toHaveBeenCalledTimes(1));
    expect(publish.mock.calls[0]![0]).toEqual({
      kind: "theme", name: "enter-kit", source, version: "0.2.0", summary: "Keyboard release",
      description: "", repoUrl: "", tags: ["theme", "keyboard"], visibility,
    });
    expect(screen.getByRole<HTMLButtonElement>("button", { name: t("提交中…") }).disabled).toBe(true);
    expect(field("source").disabled).toBe(true);
    await user.keyboard("{Enter}{Enter}");
    expect(publish).toHaveBeenCalledTimes(1);
    await act(async () => finish());
    expect(await screen.findByText(t(visibility === "private" ? "已保存 {slug} {version}，仅自己可见" : "已提交 {slug} {version}", { slug: "demo/enter-kit", version: "0.2.0" }))).toBeTruthy();
    await user.click(screen.getByRole("button", { name: t("查看我的发布") }));
    expect(onMine).toHaveBeenCalledTimes(1);
  },
);

it.each(["zh", "en"])("does not submit an incomplete draft with Enter (%s)", async (lang) => {
  const { port, user } = setup(lang);
  const publish = vi.spyOn(port, "publishMarket");
  await user.type(field("name"), "enter-kit{Enter}");
  expect(publish).not.toHaveBeenCalled();
  await user.clear(field("name"));
  await user.type(field("source"), source);
  await user.keyboard("{Enter}");
  expect(publish).not.toHaveBeenCalled();
});

it.each(["zh", "en"].flatMap((lang) => ["public", "private"].flatMap((visibility) => [false, true].map((reuse) => ({ lang, visibility, reuse })))))(
  "keeps Enter in tag and metadata fields out of submission ($lang/$visibility/reuse=$reuse)",
  async ({ lang, visibility, reuse }) => {
    const initial: MarketPackage | undefined = reuse ? {
      kind: "skill", handle: "demo", name: "enter-kit", slug: "demo/enter-kit", tags: ["existing"],
      summary: "", description: "", homepage: "", repoUrl: "", status: "active", latestVersion: "0.1.0", updatedAt: "",
      installCount: 0, starCount: 0, upCount: 0, downCount: 0, approvalRate: null, score: 0, verified: false,
    } : undefined;
    const { port, user } = setup(lang, initial);
    const publish = vi.spyOn(port, "publishMarket");
    if (!reuse) await user.type(field("name"), "enter-kit");
    await user.type(field("source"), source);
    if (visibility === "private") await user.click(screen.getByRole("checkbox"));
    screen.getByRole("checkbox").focus();
    await user.keyboard("{Enter}");
    expect(publish).not.toHaveBeenCalled();
    const tags = document.querySelectorAll<HTMLInputElement>('input[data-value="tags"]');
    if (reuse) {
      await user.type(tags[0], "-edited{Enter}");
      expect(publish).not.toHaveBeenCalled();
      expect(tags[0].value).toBe("existing-edited");
    }
    expect(fireEvent.keyDown(tags[tags.length - 1], { key: "Enter", isComposing: true })).toBe(true);
    await user.type(tags[tags.length - 1], "new-one，new-two{Enter}{Enter}");
    expect(publish).not.toHaveBeenCalled();
    expect(tags[tags.length - 1].value).toBe("new-one，new-two");
    for (const [name, value] of [["version", "0.2.0"], ["summary", "Keyboard release"], ["repoUrl", "https://example.test/repo"]]) {
      await user.type(field(name), value + "{Enter}");
      expect(publish).not.toHaveBeenCalled();
      expect(field(name).value).toBe(value);
    }
    await user.click(screen.getByRole("button", { name: t(visibility === "private" ? "保存为私有" : "提交审核") }));
    expect(publish).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({
      name: "enter-kit", source, visibility, version: "0.2.0", summary: "Keyboard release", repoUrl: "https://example.test/repo",
      tags: [...(reuse ? ["existing-edited"] : []), "new-one", "new-two"],
    }));
  },
);

it.each(["zh", "en"])("keeps kind activation and description Enter out of submission (%s)", async (lang) => {
  const { port, user } = setup(lang);
  const publish = vi.spyOn(port, "publishMarket");
  await user.type(field("name"), "enter-kit");
  await user.type(field("source"), source);
  screen.getByRole("radio", { name: t("主题") }).focus();
  await user.keyboard("{Enter}");
  expect(screen.getByRole("radio", { name: t("主题") }).getAttribute("aria-checked")).toBe("true");
  const description = document.querySelector<HTMLTextAreaElement>("textarea")!;
  await user.type(description, "line one{Enter}line two");
  expect(description.value).toBe("line one\nline two");
  expect(publish).not.toHaveBeenCalled();
});

it.each(["zh", "en"])("preserves the draft and retries with Enter after a failed write (%s)", async (lang) => {
  const { port, user } = setup(lang);
  const original = port.publishMarket.bind(port);
  const publish = vi.spyOn(port, "publishMarket").mockRejectedValueOnce(new Error("registry offline")).mockImplementation(original);
  await user.type(field("name"), "enter-kit");
  await user.type(field("source"), source);
  await user.keyboard("{Enter}");
  expect(await screen.findByText("registry offline")).toBeTruthy();
  expect(field("name").value).toBe("enter-kit");
  expect(field("source").value).toBe(source);
  expect(field("source").disabled).toBe(false);
  await user.keyboard("{Enter}");
  expect(await screen.findByText(t("已提交 {slug} {version}", { slug: "demo/enter-kit", version: "0.1.0" }))).toBeTruthy();
  expect(publish).toHaveBeenCalledTimes(2);
});
