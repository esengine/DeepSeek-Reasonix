// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, waitFor } from "@testing-library/react";
import "./testkit";
import { App } from "./App";
import { MockHub } from "../port/mock_hub";

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

// A hub whose ports are past onboarding and report every health acknowledgement.
function watchedHub() {
  const hub = new MockHub();
  const said: number[] = [];
  const build = hub.portFor.bind(hub);
  hub.portFor = (rt) => {
    const port = build(rt);
    port.providerSetup = async () => null;
    port.acknowledgeLaunchHealth = async () => {
      said.push(Date.now());
    };
    return port;
  };
  return { hub, said };
}

// The swap is performed by a process that cannot judge the result, so the
// rollback material is kept until the application that came up reports in.
// Nothing did: the endpoint existed and no client in the tree ever called it,
// so the transaction was never closed and every later install refused with "a
// pending update already exists" — permanently, after the first one worked.
describe("a launch that comes up says so", () => {
  it("retires the update it booted from, once", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const { hub, said } = watchedHub();
    render(<App hub={hub} />);

    // Not on the first frame: a build that comes up and dies immediately must
    // not be the one that throws away the way back to the build before it.
    await vi.advanceTimersByTimeAsync(500);
    expect(said, "acknowledged before the probation was served").toHaveLength(0);

    await vi.advanceTimersByTimeAsync(3000);
    await waitFor(() => expect(said.length, "never acknowledged; the transaction stays open").toBe(1));

    // Once for the launch, not once per pane the user opens.
    await vi.advanceTimersByTimeAsync(5000);
    expect(said, "said again for something that is not a launch").toHaveLength(1);
  });

  // An application still asking for a key has not come up. Retiring the way
  // back from inside onboarding would discard it on a launch the user cannot
  // yet use.
  it("says nothing while onboarding is still on screen", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const hub = new MockHub();
    const said: number[] = [];
    const build = hub.portFor.bind(hub);
    hub.portFor = (rt) => {
      const port = build(rt);
      port.acknowledgeLaunchHealth = async () => {
        said.push(Date.now());
      };
      return port; // providerSetup left requiring a key
    };

    render(<App hub={hub} />);
    await vi.advanceTimersByTimeAsync(6000);
    expect(said, "retired the rollback material from inside onboarding").toHaveLength(0);
  });
});
