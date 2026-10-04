// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { boot, STORAGE, t } from "../i18n";
import { MockPort } from "../port/mock";
import type { AgentPort, MarketPackage } from "../port/port";
import { MyPackages } from "./MarketPublish";

afterEach(() => { cleanup(); localStorage.setItem(STORAGE, "zh"); boot(); });

it.each(["zh", "en"].flatMap((lang) => ["success", "failure"].map((outcome) => ({ lang, outcome }))))(
  "keeps the review result after an earlier install refresh $outcome ($lang)",
  async ({ lang, outcome }) => {
    localStorage.setItem(STORAGE, lang);
    boot();
    const port = new MockPort() as unknown as AgentPort;
    const published = await port.publishMarket({
      kind: "theme", name: "review-refresh-demo", visibility: "private",
      source: "https://github.com/demo/themes/tree/" + "a".repeat(40) + "/dusk",
    });
    const read = port.myMarket.bind(port);
    const submit = port.submitMarket.bind(port);
    let finishReview!: () => Promise<MarketPackage>;
    const review = vi.spyOn(port, "submitMarket").mockImplementation((slug) => new Promise((resolve) => {
      finishReview = async () => { const pkg = await submit(slug); resolve(pkg); return pkg; };
    }));
    let finishList!: () => void;
    const refresh = vi.spyOn(port, "myMarket").mockImplementationOnce(read).mockImplementationOnce(async () => {
      const rows = await read();
      return new Promise((resolve, reject) => {
        finishList = () => outcome === "success" ? resolve(rows) : reject(new Error("old inventory failed"));
      });
    });
    const onInstalled = vi.fn();
    render(<MyPackages port={port} onInstalled={onInstalled} />);
    const row = (await screen.findByText(published.package.name)).closest("li")!;
    await userEvent.click(within(row).getByRole("button", { name: t("提交审核") }));
    await userEvent.click(within(row).getByRole("button", { name: t("安装") }));
    await userEvent.click(await screen.findByRole("button", { name: t("安装") }));
    await waitFor(() => expect(onInstalled).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(refresh).toHaveBeenCalledTimes(2));
    await act(async () => { await finishReview(); });
    await act(async () => finishList());
    await userEvent.click(screen.getByRole("button", { name: t("返回我的发布") }));
    const current = (await screen.findByText(published.package.name)).closest("li")!;
    expect(within(current).getByText(t("审核中"))).toBeTruthy();
    expect(within(current).getByText(t("已安装"))).toBeTruthy();
    expect(within(current).queryByRole("button", { name: t("提交审核") })).toBeNull();
    expect(screen.queryByText("old inventory failed")).toBeNull();
    expect(review).toHaveBeenCalledTimes(1);
    expect(onInstalled).toHaveBeenCalledTimes(1);
  },
);
