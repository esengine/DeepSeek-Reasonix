import { describe, expect, it } from "vitest";
import { planProtocolSwitch } from "./protocolswitch";
import type { ModelEntry } from "../port/port";

const m = (provider: string, model: string, kind: string): ModelEntry =>
  ({ ref: `${provider}/${model}`, provider, model, kind }) as ModelEntry;

// One relay reached through two doors: the OpenAI one carries everything it
// serves, the Anthropic one only the Claude models.
const OPENAI = [m("yyds", "claude-opus-5", "openai"), m("yyds", "gpt-5.6-luna", "openai")];
const ANTHROPIC = [m("yyds-anthropic", "claude-opus-5", "anthropic")];
const ALL = [...OPENAI, ...ANTHROPIC];
const vendor = { byKind: { openai: OPENAI, anthropic: ANTHROPIC } };

describe("picking the other door", () => {
  it("switches the runtime when the running model exists on both", () => {
    expect(planProtocolSwitch(vendor, ALL, "yyds/claude-opus-5", "anthropic")).toEqual({
      do: "switch",
      ref: "yyds-anthropic/claude-opus-5",
    });
  });

  // The bug: on gpt-5.6-luna the Anthropic door has no counterpart, so the
  // runtime cannot follow. Moving the control anyway is what read as "it did
  // not persist" — the next reload derives the door back from the running
  // model and the control snapped back with nothing said.
  it("stays put when the other door does not carry the running model", () => {
    expect(planProtocolSwitch(vendor, ALL, "yyds/gpt-5.6-luna", "anthropic")).toEqual({
      do: "stay",
      model: "gpt-5.6-luna",
    });
  });

  it("only filters the list for an account nothing is running on", () => {
    expect(planProtocolSwitch(vendor, ALL, "deepseek/deepseek-v4-pro", "anthropic")).toEqual({ do: "show" });
    expect(planProtocolSwitch(undefined, ALL, "yyds/claude-opus-5", "anthropic")).toEqual({ do: "show" });
  });

  it("does not re-issue a switch to the door already in use", () => {
    expect(planProtocolSwitch(vendor, ALL, "yyds/claude-opus-5", "openai")).toEqual({ do: "show" });
  });
});
