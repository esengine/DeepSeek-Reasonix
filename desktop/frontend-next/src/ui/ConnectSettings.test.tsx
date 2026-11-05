// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import "./testkit";
import { MockHub } from "../port/mock_hub";
import { MockPort } from "../port/mock";
import type { AgentPort } from "../port/port";
import { ConnectSettings } from "./ConnectSettings";
import { SettingDeepContext } from "./SettingDeep";

afterEach(cleanup);

function draw(remote: boolean) {
  const go = vi.fn();
  const port = new MockPort() as unknown as AgentPort;
  render(
    <SettingDeepContext.Provider value={{ pages: true, open: null, section: "连接", go }}>
      <ConnectSettings port={port} networkPort={port} host="" hub={new MockHub()} onError={() => {}} remote={remote} />
    </SettingDeepContext.Provider>,
  );
  return go;
}

it("is a network card and an other-machines card of one row per setting", () => {
  draw(true);
  expect([...document.querySelectorAll(".prefs-card > header h3")].map((h) => h.textContent)).toEqual(["网络", "其他机器"]);
  for (const id of ["network", "remote", "phone"]) expect(document.querySelector(`[data-setting="${id}"]`), id).toBeTruthy();
});

it("opens each page from its pill", () => {
  const go = draw(true);
  for (const [name, id] of [["详细设置", "network"], ["远程机器", "remote"], ["设置手机访问", "phone"]] as const) {
    fireEvent.click(screen.getByRole("button", { name }));
    expect(go).toHaveBeenLastCalledWith(id);
  }
});

it("draws no machines card where remote access is not offered", () => {
  draw(false);
  expect(document.querySelector('[data-setting="remote"]')).toBeNull();
});
