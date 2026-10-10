// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { boot, STORAGE } from "../i18n";
import type { Protocol, ProviderEntry } from "../port/port";
import { AddProvider } from "./AddProvider";
import { EditConn } from "./EditConn";
import { Providers, type Port } from "./Providers";
import { effortExample } from "./provider_compat";

const setLocale = (lang: "zh" | "en") => { localStorage.setItem(STORAGE, lang); boot(); };

beforeEach(() => {
  const values = new Map<string, string>();
  vi.stubGlobal("localStorage", {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => void values.set(key, value),
  });
});
afterEach(() => {
  cleanup();
  setLocale("zh");
  vi.unstubAllGlobals();
});

const catalog: Protocol[] = [
  { kind: "openai", discovery: "openai", serverWebSearch: false, reasoningParams: true, effortField: "reasoning_effort", effortUnder: ["openai"] },
  { kind: "responses", discovery: "openai", serverWebSearch: true, reasoningParams: true, effortField: "reasoning.effort", effortUnder: [] },
  { kind: "odd-wire", discovery: "odd", serverWebSearch: false, reasoningParams: true, effortField: "a.b.c", effortUnder: [] },
];

const entry = (kind: string, over: Partial<ProviderEntry> = {}): ProviderEntry => ({
  name: `${kind}-relay`, kind, baseUrl: `https://${kind}.example/v1`, models: ["gpt-5.5"], default: "gpt-5.5",
  hasKey: true, inUse: false, preset: false, canSetThinking: true, sendsThinking: true,
  effortField: catalog.find((p) => p.kind === kind)?.effortField, ...over,
});

const thinkingSelect = () => screen.getAllByRole("combobox").find((c) => c.querySelector('option[value="none"]')) as HTMLSelectElement;

const editConn = (e: ProviderEntry) => render(
  <EditConn entry={e} port={{ editProvider: vi.fn() } as unknown as Port} busy="" setBusy={() => {}} onDone={() => {}} onRevert={() => {}} declare />,
);

describe("effortExample", () => {
  it("nests the example along the declared dotted path", () => {
    expect(effortExample("reasoning_effort", "high")).toBe('{"reasoning_effort":"high"}');
    expect(effortExample("reasoning.effort", "xhigh")).toBe('{"reasoning":{"effort":"xhigh"}}');
    expect(effortExample("a.b.c", "x")).toBe('{"a":{"b":{"c":"x"}}}');
  });
});

describe("the edit form words the thinking parameter from the protocol's declaration", () => {
  it.each([
    ["zh", "openai", "当前接口类型会按 reasoning_effort 发送，例如 {\"reasoning_effort\":\"high\"}"],
    ["zh", "responses", "当前接口类型会按 reasoning.effort 发送，例如 {\"reasoning\":{\"effort\":\"high\"}}"],
    ["en", "openai", "This interface type sends the level as reasoning_effort, for example {\"reasoning_effort\":\"high\"}"],
    ["en", "responses", "This interface type sends the level as reasoning.effort, for example {\"reasoning\":{\"effort\":\"high\"}}"],
    ["en", "odd-wire", "This interface type sends the level as a.b.c, for example {\"a\":{\"b\":{\"c\":\"high\"}}}"],
  ] as const)("%s %s", (lang, kind, line) => {
    setLocale(lang);
    editConn(entry(kind));
    expect(screen.getByText(line)).toBeTruthy();
    const options = within(thinkingSelect()).getAllByRole("option").map((o) => o.textContent);
    expect(options.join("|")).not.toMatch(/reasoning_effort|reasoning\.effort/);
  });

  it("uses the first hand-filled level in the example and sends it as written", async () => {
    editConn(entry("responses"));
    await userEvent.type(screen.getByPlaceholderText("low, medium, high"), "Minimal, xhigh");
    expect(screen.getByText('当前接口类型会按 reasoning.effort 发送，例如 {"reasoning":{"effort":"minimal"}}')).toBeTruthy();
    expect(screen.getByText(/按原样作为 reasoning\.effort 的值发送/)).toBeTruthy();
  });

  it("keeps the option that sends no thinking parameter, and drops the shape line under it", async () => {
    editConn(entry("responses"));
    const select = thinkingSelect();
    expect(within(select).getByRole("option", { name: "不发思考参数" })).toBeTruthy();
    await userEvent.selectOptions(select, "none");
    expect(screen.queryByText(/当前接口类型会按/)).toBeNull();
  });

  it("says nothing when the kernel resolved the entry to a shape that is not the wire's own", () => {
    editConn(entry("openai", { effortField: undefined, reasoningProtocol: "glm" }));
    expect(screen.queryByText(/当前接口类型会按/)).toBeNull();
  });

  it("drops the line while the picked protocol differs from the saved one", async () => {
    editConn(entry("openai", { reasoningProtocol: "openai" }));
    expect(screen.getByText(/当前接口类型会按 reasoning_effort/)).toBeTruthy();
    await userEvent.selectOptions(thinkingSelect(), "glm");
    expect(screen.queryByText(/当前接口类型会按/)).toBeNull();
  });

  it("says nothing about a field the kernel did not declare", () => {
    editConn(entry("openai", { effortField: undefined }));
    expect(screen.queryByText(/当前接口类型会按/)).toBeNull();
    expect(screen.getByText(/按原样发送。填写后会替代/)).toBeTruthy();
  });
});

describe("add, edit and detail read one declaration", () => {
  it("Add provider enables the thinking switch for a Responses source and words it the same way", async () => {
    const port = { protocols: vi.fn(async () => catalog), saveProvider: vi.fn() } as unknown as Port;
    render(<AddProvider port={port} taken={[]} known={[]} onDone={() => {}} onCancel={() => {}} />);
    await userEvent.selectOptions(await screen.findByLabelText("接口协议"), "responses");
    await userEvent.click(screen.getByText("高级连接选项"));
    const sw = screen.getByRole("switch", { name: "发送思考控制" }) as HTMLButtonElement;
    expect(sw.disabled).toBe(false);
    expect(sw.getAttribute("aria-checked")).toBe("true");
    expect(screen.getByText('当前接口类型会按 reasoning.effort 发送，例如 {"reasoning":{"effort":"high"}}')).toBeTruthy();
  });

  it("Add provider shows a Chat shape line only under a protocol the wire declares", async () => {
    const port = { protocols: vi.fn(async () => catalog), saveProvider: vi.fn() } as unknown as Port;
    render(<AddProvider port={port} taken={[]} known={[]} onDone={() => {}} onCancel={() => {}} />);
    await screen.findByLabelText("接口协议");
    await userEvent.click(screen.getByText("高级连接选项"));
    expect(screen.queryByText(/当前接口类型会按/)).toBeNull();
    const pick = screen.getAllByRole("combobox").find((c) => c.querySelector('option[value="glm"]')) as HTMLSelectElement;
    await userEvent.selectOptions(pick, "openai");
    expect(screen.getByText(/当前接口类型会按 reasoning_effort/)).toBeTruthy();
    await userEvent.selectOptions(pick, "glm");
    expect(screen.queryByText(/当前接口类型会按/)).toBeNull();
  });

  it("the detail page shows the thinking row for a Responses source and words it the same way", async () => {
    const port = {
      providers: vi.fn(async () => [entry("responses")]),
      protocols: vi.fn(async () => catalog),
      editProvider: vi.fn(),
    } as unknown as Port;
    render(<Providers port={port} onChanged={() => {}} onFailed={() => {}} protocol={{}} onProtocol={() => {}} activeKindFor={(a) => a.kinds[0]} />);
    const detail = await screen.findByRole("region");
    await waitFor(() => expect(within(detail).getByRole("group", { name: /的思考参数/ })).toBeTruthy());
    expect(within(detail).getAllByText('当前接口类型会按 reasoning.effort 发送，例如 {"reasoning":{"effort":"high"}}').length).toBeGreaterThan(0);
  });
});
