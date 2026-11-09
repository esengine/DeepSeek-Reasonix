// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import "./testkit";
import { MockHub } from "../port/mock_hub";
import { MockPort } from "../port/mock";
import type { AgentPort } from "../port/port";
import { ConnectSettings } from "./ConnectSettings";

afterEach(cleanup);

function draw(remote: boolean) {
  const port = new MockPort() as unknown as AgentPort;
  render(
    <>
      <ConnectSettings port={port} networkPort={port} host="" hub={new MockHub()} onError={() => {}} remote={remote} />
    </>,
  );
}

it("is a network card and an other-machines card of one row per setting", () => {
  draw(true);
  expect([...document.querySelectorAll(".prefs-card > header h3")].map((h) => h.textContent)).toEqual(["网络", "其他机器"]);
  for (const id of ["network", "remote", "phone"]) expect(document.querySelector(`[data-setting="${id}"]`), id).toBeTruthy();
});

it("expands network and machine controls in their cards", async () => {
  draw(true);
  expect(await screen.findByRole("radiogroup", { name: "代理模式" })).toBeTruthy();
  for (const id of ["remote", "phone"]) expect(document.querySelector(`#set-${id} .grp-items`)).toBeTruthy();
});

it("draws no machines card where remote access is not offered", () => {
  draw(false);
  expect(document.querySelector('[data-setting="remote"]')).toBeNull();
});
