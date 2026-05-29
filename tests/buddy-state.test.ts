import { describe, expect, it } from "vitest";
import {
  BUDDY_PULSE_TTL_MS,
  buddyPhrase,
  createBuddyPulse,
  isBuddyPulseExpired,
  resolveBuddyMood,
} from "../src/cli/ui/buddy/state.js";

describe("buddy state", () => {
  it("maps idle, streaming, tool, and wait signals to moods", () => {
    expect(resolveBuddyMood({})).toBe("idle");
    expect(resolveBuddyMood({ streaming: true })).toBe("thinking");
    expect(resolveBuddyMood({ busy: true })).toBe("thinking");
    expect(resolveBuddyMood({ toolActive: true })).toBe("working");
    expect(resolveBuddyMood({ loopActive: true })).toBe("working");
    expect(resolveBuddyMood({ awaitingUser: true })).toBe("warning");
  });

  it("keeps user waits above tool and streaming activity", () => {
    expect(
      resolveBuddyMood({
        awaitingUser: true,
        toolActive: true,
        busy: true,
        streaming: true,
      }),
    ).toBe("warning");
  });

  it("lets pet pulses override every normal state", () => {
    expect(resolveBuddyMood({ awaitingUser: true, pulse: createBuddyPulse("pet", 100) })).toBe(
      "pet",
    );
  });

  it("turns idle wake pulses into thinking without overriding active work", () => {
    const pulse = createBuddyPulse("wake", 100);

    expect(resolveBuddyMood({ pulse })).toBe("thinking");
    expect(resolveBuddyMood({ toolActive: true, pulse })).toBe("working");
    expect(resolveBuddyMood({ awaitingUser: true, pulse })).toBe("warning");
  });

  it("expires local pulses on the shared ttl", () => {
    const pulse = createBuddyPulse("wake", 1000);

    expect(isBuddyPulseExpired(pulse, 1000 + BUDDY_PULSE_TTL_MS - 1)).toBe(false);
    expect(isBuddyPulseExpired(pulse, 1000 + BUDDY_PULSE_TTL_MS)).toBe(true);
  });

  it("keeps status captions centralized for the CLI and future desktop client", () => {
    expect(buddyPhrase("idle")).toBe("ready at the surface");
    expect(buddyPhrase("thinking")).toBe("diving through context");
    expect(buddyPhrase("working")).toBe("following tool wake");
    expect(buddyPhrase("warning")).toBe("waiting for your choice");
    expect(buddyPhrase("pet")).toBe("bloop");
  });
});
