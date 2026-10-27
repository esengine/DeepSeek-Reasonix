// @vitest-environment jsdom
import { afterEach, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Network } from "./Network";
import { boot, STORAGE, t } from "../i18n";
import type { AgentPort, NetworkProbe } from "../port/port";

afterEach(() => {
  cleanup();
  localStorage.setItem(STORAGE, "zh");
  boot();
});

const probes: NetworkProbe[] = [
  { step: "proxy", ok: true, detail: "经由 http://proxy.corp:8080", durationMs: 0, code: "net.proxy.via", params: { proxy: "http://proxy.corp:8080" } },
  { step: "connect", ok: false, detail: "连不上 proxy.corp:8080：refused", durationMs: 9, code: "net.connect.failed", params: { host: "proxy.corp", port: "8080", error: "refused" },
    advice: "解析得到地址但连不上代理，检查代理是不是没开、端口对不对", adviceCode: "net.connect.proxy_down" },
  { step: "auth", ok: false, detail: "kernel text with no code", durationMs: 0, advice: "no code either" },
];

async function run(lang: "zh" | "en") {
  localStorage.setItem(STORAGE, lang);
  boot();
  const port = {
    network: async () => ({ mode: "auto", effective: "x" }),
    diagnoseNetwork: async () => probes,
  } as unknown as AgentPort;
  render(<Network port={port} />);
  await userEvent.click(await screen.findByRole("button", { name: t("测试已保存配置") }));
}

it("words each probe step in English from its code, with the pieces filled in", async () => {
  await run("en");
  expect(await screen.findByText("Through http://proxy.corp:8080")).toBeTruthy();
  expect(screen.getByText("Cannot connect to proxy.corp:8080: refused")).toBeTruthy();
  expect(screen.getByText(/The name resolved but the proxy is not reachable/)).toBeTruthy();
  expect(document.querySelector(".probes")?.textContent).not.toMatch(/[\u4e00-\u9fff]/);
  expect(screen.getByText("Connection")).toBeTruthy();
});

it("words the same steps in Chinese, and shows the kernel's own text where it sent no code", async () => {
  await run("zh");
  expect(await screen.findByText("经由 http://proxy.corp:8080")).toBeTruthy();
  expect(screen.getByText("连不上 proxy.corp:8080：refused")).toBeTruthy();
  expect(screen.getByText("kernel text with no code")).toBeTruthy();
  expect(screen.getByText("no code either")).toBeTruthy();
});
