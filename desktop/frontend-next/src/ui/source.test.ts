import { describe, expect, it } from "vitest";
import { answerSource, decisionSource, messageSource } from "./source";

const phone = { id: "dev-a", ordinal: 1, machine: "desk" };

describe("where a message or a decision came from", () => {
  it("says nothing on the window about the window's own lines", () => {
    expect(messageSource(undefined, null)).toBe("");
  });

  it("names the device a line came from on the window", () => {
    expect(messageSource({ device: "dev-a", ordinal: 1 }, null)).toBe("来自 设备 1");
  });

  it("says nothing on a phone about its own lines", () => {
    expect(messageSource({ device: "dev-a", ordinal: 1 }, phone)).toBe("");
  });

  it("tells a phone which lines the computer sent, and which another phone did", () => {
    expect(messageSource(undefined, phone)).toBe("来自 电脑");
    expect(messageSource({ device: "dev-b", ordinal: 2 }, phone)).toBe("来自 设备 2");
  });

  it("names who settled a prompt this screen did not answer", () => {
    expect(decisionSource({ device: "dev-b", ordinal: 2 }, null)).toBe("由 设备 2 处理");
    expect(decisionSource("window", phone)).toBe("在电脑上处理");
    expect(decisionSource("window", null)).toBe("");
    expect(decisionSource(undefined, phone)).toBe("");
  });
});

describe("who answered a question elsewhere", () => {
  it("names the device and what it chose", () => {
    expect(answerSource({ device: "dev-b", ordinal: 2 }, null, "框架: React")).toBe("由 设备 2 回答：框架: React");
  });

  it("tells a phone the computer answered", () => {
    expect(answerSource("window", { id: "dev-a", ordinal: 1, machine: "m" }, "")).toBe("在电脑上回答");
  });

  it("leaves the window's old line where nothing better is known", () => {
    expect(answerSource("window", null, "x")).toBe("");
  });
});
