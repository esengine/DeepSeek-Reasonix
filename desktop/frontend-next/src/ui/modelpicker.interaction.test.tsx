// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import "./testkit";
import { Picker } from "./Menu";
import { modelMenu } from "./modelmenu";
import { boot } from "../i18n";
import type { ModelEntry } from "../port/port";

afterEach(() => {
  cleanup();
  localStorage.clear();
  boot();
});

function fleet(perProvider: number, hosts = ["api.deepseek.com", "api.moonshot.cn", "relay.example.com", "gw.acme.io"]): ModelEntry[] {
  return hosts.flatMap((host) => {
    const name = host.split(".")[0] === "api" ? host.split(".")[1] : host.split(".")[0];
    return Array.from({ length: perProvider }, (_, i) => ({
      ref: `${name}/m${i}`,
      provider: name,
      vendor: host,
      model: `${name}-model-${i}`,
      kind: "openai",
      vision: i === 0 ? true : undefined,
    }));
  });
}

function mount(models: ModelEntry[], current?: string, onPick = vi.fn()) {
  render(
    <Picker
      label="model"
      place="bottom"
      current={current}
      items={modelMenu(models)}
      searchAlways
      searchPlaceholder="Search models"
      menuClassName="studio-model-menu"
      onPick={onPick}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "model" }));
  return onPick;
}

const rows = () => [...document.querySelectorAll<HTMLElement>(".studio-model-menu button.mi:not(.plain)")];
const heads = () => [...document.querySelectorAll<HTMLElement>(".studio-model-menu .mi.head .lb")].map((n) => n.textContent);

describe("model picker", () => {
  it("shows the search field even for a short list, focused on open", () => {
    mount(fleet(1));
    const field = screen.getByPlaceholderText("Search models");
    expect(field).toBe(document.activeElement);
  });

  it("groups 16 models under four provider headings, each with a mark", () => {
    mount(fleet(4));
    expect(heads()).toEqual(["deepseek", "moonshot", "relay", "gw"]);
    expect(rows()).toHaveLength(16);
    expect(document.querySelectorAll(".studio-model-menu .mi.head .mi-mono")).toHaveLength(4);
    expect(document.querySelectorAll(".studio-model-menu button.mi .mi-mono")).toHaveLength(16);
  });

  it("gives a provider the same letter and tone every time it is drawn", () => {
    mount(fleet(2));
    const first = [...document.querySelectorAll<HTMLElement>(".mi.head .mi-mono")].map((n) => `${n.textContent}${n.dataset.tone}`);
    cleanup();
    mount(fleet(2));
    const again = [...document.querySelectorAll<HTMLElement>(".mi.head .mi-mono")].map((n) => `${n.textContent}${n.dataset.tone}`);
    expect(again).toEqual(first);
    expect(first[0]).toMatch(/^D\d$/);
  });

  it("keeps a heading for a lone provider so the endpoint stays visible", () => {
    mount(fleet(3, ["api.deepseek.com"]));
    expect(heads()).toEqual(["deepseek"]);
  });

  it("marks the current model and nothing else", () => {
    mount(fleet(4), "relay/m2");
    const on = rows().filter((r) => r.hasAttribute("data-on"));
    expect(on).toHaveLength(1);
    expect(on[0].dataset.value).toBe("relay/m2");
  });

  it("scrolls the current model into view on open", () => {
    const spy = vi.spyOn(Element.prototype, "scrollIntoView");
    mount(fleet(15), "gw/m14");
    expect(spy).toHaveBeenCalled();
    expect((spy.mock.contexts.at(-1) as HTMLElement).dataset.value).toBe("gw/m14");
    spy.mockRestore();
  });

  it("filters by model name and by provider, dropping emptied groups", () => {
    mount(fleet(4));
    const field = screen.getByPlaceholderText("Search models");
    fireEvent.change(field, { target: { value: "moonshot" } });
    expect(heads()).toEqual(["moonshot"]);
    expect(rows()).toHaveLength(4);
    fireEvent.change(field, { target: { value: "relay model-2" } });
    expect(rows().map((r) => r.dataset.value)).toEqual(["relay/m2"]);
  });

  it("says so when nothing matches, and Enter then picks nothing", () => {
    const onPick = mount(fleet(4));
    const field = screen.getByPlaceholderText("Search models");
    fireEvent.change(field, { target: { value: "zzzz" } });
    const note = screen.getByText("Nothing matches");
    const manage = screen.getByRole("menuitem", { name: /Manage models and connections/ });
    expect(note.compareDocumentPosition(manage) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(rows()).toHaveLength(0);
    fireEvent.keyDown(field, { key: "Enter" });
    expect(onPick).not.toHaveBeenCalled();
    expect(screen.getByRole("menuitem", { name: /Manage models and connections/ })).toBeTruthy();
  });

  it("Enter takes the first match", () => {
    const onPick = mount(fleet(4));
    const field = screen.getByPlaceholderText("Search models");
    fireEvent.change(field, { target: { value: "model-3" } });
    fireEvent.keyDown(field, { key: "Enter" });
    expect(onPick).toHaveBeenCalledWith("deepseek/m3");
  });

  it("arrows walk the rows, skipping headings, and Up from the first row returns to the field", () => {
    mount(fleet(2));
    const menu = screen.getByRole("menu");
    const field = screen.getByPlaceholderText("Search models");
    fireEvent.keyDown(field, { key: "ArrowDown" });
    const [a, b, c] = rows();
    expect(document.activeElement).toBe(a);
    fireEvent.keyDown(menu, { key: "ArrowDown" });
    expect(document.activeElement).toBe(b);
    fireEvent.keyDown(menu, { key: "ArrowDown" });
    expect(document.activeElement).toBe(c);
    fireEvent.keyDown(c, { key: "ArrowUp" });
    fireEvent.keyDown(b, { key: "ArrowUp" });
    expect(document.activeElement).toBe(a);
    fireEvent.keyDown(a, { key: "ArrowUp" });
    expect(document.activeElement).toBe(field);
  });

  it("typing while a row has focus goes back to the field", () => {
    mount(fleet(2));
    fireEvent.keyDown(screen.getByPlaceholderText("Search models"), { key: "ArrowDown" });
    fireEvent.keyDown(rows()[0], { key: "k" });
    expect(document.activeElement).toBe(screen.getByPlaceholderText("Search models"));
  });

  it("picks a row on click and closes", () => {
    const onPick = mount(fleet(4));
    fireEvent.click(rows()[5]);
    expect(onPick).toHaveBeenCalledWith("moonshot/m1");
    expect(screen.getByRole("menu", { hidden: true }).hidden).toBe(true);
  });

  it("Escape closes the menu and clears the query for next time", () => {
    mount(fleet(4));
    const field = screen.getByPlaceholderText("Search models");
    fireEvent.change(field, { target: { value: "relay" } });
    fireEvent.keyDown(window, { key: "Escape" });
    fireEvent.click(screen.getByRole("button", { name: "model" }));
    expect((screen.getByPlaceholderText("Search models") as HTMLInputElement).value).toBe("");
    expect(rows()).toHaveLength(16);
  });

  it("holds 60 models in one scrolling list without dropping any", () => {
    mount(fleet(15));
    expect(rows()).toHaveLength(60);
    expect(heads()).toHaveLength(4);
  });

  it("keeps the management action reachable below a long list", () => {
    mount(fleet(15));
    const all = [...document.querySelectorAll(".studio-model-menu .mi")];
    expect(all.at(-1)?.textContent).toMatch(/Manage models/);
  });

  it("shows the kind on every row and a reads-images badge only where declared", () => {
    mount(fleet(2));
    const first = rows()[0];
    expect(within(first).getByText("OpenAI-compatible")).toBeTruthy();
    expect(within(first).getByText("Reads images")).toBeTruthy();
    expect(within(rows()[1]).queryByText("Reads images")).toBeNull();
  });

  it("does not remount rows when typing narrows then restores the list", () => {
    mount(fleet(3));
    const kept = rows()[0];
    const field = screen.getByPlaceholderText("Search models");
    fireEvent.change(field, { target: { value: "deepseek" } });
    fireEvent.change(field, { target: { value: "" } });
    expect(rows()[0]).toBe(kept);
  });

  it("renders in Chinese too", () => {
    localStorage.setItem("rx-lang", "zh");
    boot();
    render(<Picker label="m" place="bottom" items={modelMenu(fleet(2))} searchAlways onPick={() => {}} menuClassName="studio-model-menu" />);
    fireEvent.click(screen.getByRole("button", { name: "m" }));
    expect(screen.getByText("管理模型与连接")).toBeTruthy();
    expect(screen.getAllByText("OpenAI 兼容", { selector: ".studio-item-meta" }).length).toBe(8);
  });
});
