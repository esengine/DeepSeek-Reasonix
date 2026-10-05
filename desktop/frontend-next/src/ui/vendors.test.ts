import { describe, expect, it } from "vitest";
import { accountLabel, disambiguate, nameFrom, sourceNameUsable, vendorLabel } from "./vendors";

describe("nameFrom", () => {
  it("derives the config name from the host", () => {
    expect(nameFrom("https://api.moonshot.cn/v1")).toBe("moonshot");
    expect(nameFrom("https://apihub.agnes-ai.com/v1")).toBe("apihub");
    expect(nameFrom("https://gateway.relay.example/v1")).toBe("relay");
    expect(nameFrom("not a url")).toBe("custom");
  });

  it("agrees with vendorLabel, so a saved name stays classifiable", () => {
    for (const url of ["https://api.deepseek.com/v1", "https://open.bigmodel.cn/api/paas/v4"]) {
      expect(nameFrom(url)).toBe(vendorLabel(new URL(url).hostname));
    }
  });
});

describe("accountLabel", () => {
  const host = "apihub.agnes-ai.com";

  it("shows the name the user typed", () => {
    expect(accountLabel(host, [{ name: "Agnes" }])).toBe("Agnes");
  });

  it("falls back to the host when the prefilled name was kept", () => {
    expect(accountLabel(host, [{ name: "apihub" }])).toBe("apihub");
  });

  it("treats the uniquifier as ours, not as a choice", () => {
    expect(accountLabel(host, [{ name: "apihub-2" }])).toBe("apihub");
  });

  it("leaves curated accounts named after their vendor", () => {
    const doors = [
      { name: "deepseek", preset: true },
      { name: "deepseek-anthropic", preset: true },
    ];
    expect(accountLabel("api.deepseek.com", doors)).toBe("deepseek");
    expect(accountLabel("api.moonshot.cn", [{ name: "kimi-cn", preset: true }])).toBe("moonshot");
  });

  it("puts a display name ahead of every name, curated or typed", () => {
    const doors = [{ name: "deepseek", preset: true }, { name: "deepseek-anthropic", preset: true, displayName: " 公司账号 " }];
    expect(accountLabel("api.deepseek.com", doors)).toBe("公司账号");
    expect(accountLabel(host, [{ name: "Agnes", displayName: "   " }])).toBe("Agnes");
  });

  it("finds the chosen name behind a door the config lists first", () => {
    const doors = [{ name: "apihub" }, { name: "Agnes" }];
    expect(accountLabel(host, doors)).toBe("Agnes");
  });
});

describe("disambiguate", () => {
  it("adds the entry name only when two accounts share a host", () => {
    const one = disambiguate([{ host: "h", label: "Agnes", hint: "Agnes" }]);
    expect(one[0].label).toBe("Agnes");

    const two = disambiguate([
      { host: "h", label: "apihub", hint: "apihub" },
      { host: "h", label: "apihub", hint: "work" },
    ]);
    expect(two.map((v) => v.label)).toEqual(["apihub", "apihub · work"]);
  });

  it("leaves two accounts on one host alone once the user named them apart", () => {
    const named = disambiguate([
      { host: "h", label: "个人", hint: "apihub" },
      { host: "h", label: "公司", hint: "apihub-2" },
    ]);
    expect(named.map((v) => v.label)).toEqual(["个人", "公司"]);
  });
});

describe("sourceNameUsable", () => {
  it.each([
    ["", false], ["   ", false],
    ["a", true], ["a".repeat(64), true], ["a".repeat(65), false],
    [".a", false], ["-a", false], ["_a", false], ["a.", true], ["a b", false], ["a/b", false],
    ["公司中转站", false], ["ｒｅｌａｙ", false], ["relay\u0301", false], ["relay-1.x_y", true],
    [" company-relay.1_x ", true], ["Relay_1.x-y", true],
  ])("%j -> %s", (name, ok) => expect(sourceNameUsable(name)).toBe(ok));
});
