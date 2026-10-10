// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import "./testkit";
import { boot, STORAGE } from "../i18n";
import { AddProvider } from "./AddProvider";
import { SsePort } from "../port/sse";
import { checkFailure } from "./provider_check";

beforeEach(() => {
  const values = new Map<string, string>([[STORAGE, "zh"]]);
  vi.stubGlobal("localStorage", {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => void values.set(key, value),
  });
  boot();
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

const hint = /不要带 \/v1\/systemone/;

it("a decision protocol tells the person which address shape it expects, and a chat protocol does not", async () => {
  vi.stubGlobal("fetch", vi.fn(async (url: string) => {
    if (url === "/kernel/providers/protocols") return Response.json([
      { kind: "openai", answers: "chat", discovery: "openai", serverWebSearch: false, reasoningParams: true },
      { kind: "typesafe", answers: "decision", discovery: "", serverWebSearch: false, reasoningParams: false },
    ]);
    throw new Error(`Unexpected request: ${url}`);
  }));
  render(<AddProvider port={new SsePort("/kernel")} taken={[]} known={[]} onDone={() => {}} onCancel={() => {}} />);
  const kind = await screen.findByLabelText<HTMLSelectElement>("接口协议");
  await waitFor(() => expect(kind.value).toBe("openai"));
  expect(screen.queryByText(hint)).toBeNull();
  fireEvent.change(kind, { target: { value: "typesafe" } });
  expect(await screen.findByText(hint)).toBeTruthy();
  fireEvent.change(kind, { target: { value: "openai" } });
  expect(screen.queryByText(hint)).toBeNull();
});

it("a decision source failing at the wrong address is told the base-address rule, not the /v1 rule", () => {
  const said = checkFailure({ ok: false, code: "provider.probe.decision_path_not_found", httpStatus: 404 } as never);
  expect(said).toContain("基地址");
  expect(said).not.toContain("以 /v1 结尾");
  expect(said).toContain("HTTP 404");
});

it("a refused decision request keeps its status and the service's words next to a refusal sentence", () => {
  const said = checkFailure({ ok: false, code: "provider.probe.decision_rejected", httpStatus: 422, detail: "bad model" } as never);
  expect(said).toContain("拒绝了这次决策请求");
  expect(said).toContain("HTTP 422");
  expect(said).toContain("bad model");
});
