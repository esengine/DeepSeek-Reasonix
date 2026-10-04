// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { MyPackages } from "./MarketPublish";
import { MockPort } from "../port/mock";
import { boot, STORAGE, t } from "../i18n";
import type { AgentPort } from "../port/port";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  localStorage.setItem(STORAGE, "zh");
  boot();
});

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

it.each(["zh", "en"].flatMap((lang) => ["success", "failure"].map((outcome) => ({ lang, outcome }))))(
  "keeps review submission owned by one row through a $lang $outcome",
  async ({ lang, outcome }) => {
    localStorage.setItem(STORAGE, lang);
    boot();
    const port = new MockPort() as unknown as AgentPort;
    const packages = await Promise.all(["first-draft", "second-draft"].map((name) => port.publishMarket({
      kind: "theme", name, source: "https://github.com/demo/themes", visibility: "private",
    })));
    const first = packages[0]!.package;
    const second = packages[1]!.package;
    const real = port.submitMarket.bind(port);
    const pending = deferred<void>();
    const next = deferred<void>();
    const submit = vi.spyOn(port, "submitMarket")
      .mockImplementationOnce(async (slug) => { await pending.promise; return real(slug); })
      .mockImplementationOnce(async (slug) => { await next.promise; return real(slug); });
    render(<MyPackages port={port} onInstalled={() => {}} />);
    const rowOf = async (name: string) => (await screen.findByText(name)).closest("li")!;
    const review = (row: HTMLElement) => within(row).getByRole<HTMLButtonElement>("button", { name: t("提交审核") });
    const sending = (row: HTMLElement) => within(row).getByRole<HTMLButtonElement>("button", { name: t("提交中…") });

    await userEvent.click(review(await rowOf(first.name)));
    await userEvent.click(review(await rowOf(second.name)));
    await userEvent.click(sending(await rowOf(first.name)));
    expect(submit.mock.calls).toEqual([[first.slug]]);
    expect(sending(await rowOf(first.name)).disabled).toBe(true);
    expect(review(await rowOf(second.name)).disabled).toBe(true);
    expect(within(await rowOf(second.name)).getByRole<HTMLButtonElement>("button", { name: t("安装") }).disabled).toBe(false);

    await act(async () => {
      if (outcome === "success") pending.resolve();
      else pending.reject(new Error("review temporarily unavailable"));
    });
    if (outcome === "success") {
      const firstRow = await rowOf(first.name);
      await waitFor(() => expect(within(firstRow).getByText(t("审核中"))).toBeTruthy());
      expect(firstRow.querySelector('[data-action="market.submit"]')).toBeNull();
    } else {
      const firstRow = await rowOf(first.name);
      expect(review(firstRow).disabled).toBe(false);
      expect(within(firstRow).getByText("review temporarily unavailable")).toBeTruthy();
      expect(within(await rowOf(second.name)).queryByText("review temporarily unavailable")).toBeNull();
    }
    await waitFor(() => expect(review(within(document.body).getByText(second.name).closest("li")!).disabled).toBe(false));
    await userEvent.click(review(await rowOf(second.name)));
    await userEvent.click(sending(await rowOf(second.name)));
    if (outcome === "failure") await userEvent.click(review(await rowOf(first.name)));
    expect(submit.mock.calls).toEqual([[first.slug], [second.slug]]);
    expect(sending(await rowOf(second.name)).disabled).toBe(true);
    await act(async () => next.resolve());
    await waitFor(async () => expect(within(await rowOf(second.name)).getByText(t("审核中"))).toBeTruthy());
    if (outcome === "failure") {
      await userEvent.click(review(await rowOf(first.name)));
      expect(submit.mock.calls).toEqual([[first.slug], [second.slug], [first.slug]]);
      await waitFor(async () => expect(within(await rowOf(first.name)).getByText(t("审核中"))).toBeTruthy());
      expect(within(await rowOf(second.name)).getByText(t("审核中"))).toBeTruthy();
      expect(screen.queryByText("review temporarily unavailable")).toBeNull();
    }
  },
);
