// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { App } from "./App";
import { Appearance } from "./Appearance";
import { MockHub } from "../port/mock_hub";
import { MockPort } from "../port/mock";
import { setShowsSessionTabs } from "../state/prefs";

beforeEach(() => {
  localStorage.clear();
  sessionStorage.clear();
  setShowsSessionTabs(false);
  localStorage.removeItem("rx-show-session-tabs");
});
afterEach(cleanup);

async function hub(panes = 2) {
  const kernel = new MockHub();
  const build = kernel.portFor.bind(kernel);
  kernel.portFor = (rt) => {
    const port = build(rt);
    port.providerSetup = async () => null;
    return port;
  };
  if (panes > 1) await kernel.open({ sessionPath: "/sessions/second.jsonl" });
  return kernel;
}

function draw(kernel: MockHub) {
  return render(<>
    <App hub={kernel} />
    <Appearance port={new MockPort()} theme="light" onTheme={() => {}} contrast="" onContrast={() => {}}
      weight="" onWeight={() => {}} look={{}} onLook={() => {}} reloadThemes={() => {}} />
  </>);
}

const strip = () => screen.queryByRole("tablist", { name: "会话面板" });
const toggle = () => screen.getByRole("switch", { name: "显示会话标签栏" });
const settled = () => waitFor(() => {
  if (!document.querySelector(".crumb b")?.textContent) throw new Error("the active pane has not loaded");
});

describe("opt-in session tabs", () => {
  it("keeps the strip hidden by default even with two sessions open", async () => {
    draw(await hub());
    await settled();
    expect(toggle().getAttribute("aria-checked")).toBe("false");
    expect(strip()).toBeNull();
  });

  it("shows and hides the open tabs immediately through the appearance switch", async () => {
    draw(await hub());
    await settled();
    await userEvent.click(toggle());
    await waitFor(() => expect(strip()?.querySelectorAll('[role="tab"]').length).toBe(2));
    await userEvent.click(toggle());
    expect(strip()).toBeNull();
  });

  it("does not show a strip for a single session, even when enabled", async () => {
    draw(await hub(1));
    await settled();
    await userEvent.click(toggle());
    expect(toggle().getAttribute("aria-checked")).toBe("true");
    expect(strip()).toBeNull();
  });

  it("retains both enabled and disabled choices when the interface remounts", async () => {
    const kernel = await hub();
    const first = draw(kernel);
    await settled();
    await userEvent.click(toggle());
    first.unmount();

    const second = draw(kernel);
    await settled();
    expect(toggle().getAttribute("aria-checked")).toBe("true");
    expect(strip()?.querySelectorAll('[role="tab"]').length).toBe(2);
    await userEvent.click(toggle());
    second.unmount();

    draw(kernel);
    await settled();
    expect(toggle().getAttribute("aria-checked")).toBe("false");
    expect(strip()).toBeNull();
  });
});