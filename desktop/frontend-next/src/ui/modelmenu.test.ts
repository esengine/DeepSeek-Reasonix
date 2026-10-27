import { expect, it } from "vitest";
import type { ModelEntry } from "../port/port";
import { accountKey } from "./vendors";
import { modelMenu } from "./modelmenu";

it("groups the composer model menu by saved account order", () => {
  const models: ModelEntry[] = [
    { ref: "alpha/first", provider: "alpha", vendor: "alpha.example", model: "first" },
    { ref: "alpha/second", provider: "alpha", vendor: "alpha.example", model: "second" },
    { ref: "beta/third", provider: "beta", vendor: "beta.example", model: "third" },
  ];
  const items = modelMenu(models, [accountKey("beta.example"), accountKey("alpha.example")]);
  expect(items.map((item) => item.value).slice(0, 5))
    .toEqual([`__account:${accountKey("beta.example")}`, "beta/third", `__account:${accountKey("alpha.example")}`, "alpha/first", "alpha/second"]);
});

it("labels an account and its rows by the display name while refs stay on the config name", () => {
  const models: ModelEntry[] = [
    { ref: "relay/first", provider: "relay", displayName: "公司网关", vendor: "relay.example", model: "first", kind: "openai" },
    { ref: "other/second", provider: "other", vendor: "other.example", model: "second", kind: "openai" },
  ];
  const items = modelMenu(models);
  expect(items[0]).toMatchObject({ header: true, label: "公司网关", right: "relay.example" });
  expect(items[1]).toMatchObject({ value: "relay/first", label: "first" });
  expect(items[1].mono?.letter).toBe("公");
});

it("always headlines the account, even a lone one, and tones it by account key", () => {
  const items = modelMenu([{ ref: "a/x", provider: "a", vendor: "a.example", model: "x" }]);
  expect(items[0]).toMatchObject({ header: true, right: "a.example" });
  expect(items[0].mono).toEqual(items[1].mono);
  expect(items[0].mono?.tone).toBeGreaterThanOrEqual(0);
  expect(items[0].mono?.tone).toBeLessThan(5);
});

it("puts the current model first without duplicating it or changing the other accounts' order", () => {
  const models: ModelEntry[] = [
    { ref: "alpha/first", provider: "alpha", vendor: "alpha.example", model: "first", kind: "openai" },
    { ref: "alpha/second", provider: "alpha", vendor: "alpha.example", model: "second", kind: "openai" },
    { ref: "beta/third", provider: "beta", vendor: "beta.example", model: "third", kind: "anthropic" },
    { ref: "beta/fourth", provider: "beta", vendor: "beta.example", model: "fourth", kind: "anthropic" },
  ];
  const order = [accountKey("alpha.example"), accountKey("beta.example")];
  const items = modelMenu(models, order, "beta/fourth");
  expect(items[0]).toMatchObject({ value: "beta/fourth", label: "fourth" });
  expect(items[0].desc).toBe(modelMenu(models, order).find((item) => item.value === "beta/fourth")?.desc);
  expect(items.filter((item) => item.value === "beta/fourth")).toHaveLength(1);
  expect(items.slice(1, -1).map((item) => item.value)).toEqual([
    `__account:${accountKey("alpha.example")}`, "alpha/first", "alpha/second",
    `__account:${accountKey("beta.example")}`, "beta/third",
  ]);
  expect(items[1]).toMatchObject({ header: true, divide: true });
  expect(items.at(-1)).toMatchObject({ value: "__manage-models", plain: true });
  expect(models.map((m) => m.ref)).toEqual(["alpha/first", "alpha/second", "beta/third", "beta/fourth"]);
});

it("removes a now-empty account heading and follows a different current model", () => {
  const models: ModelEntry[] = [
    { ref: "alpha/first", provider: "alpha", vendor: "alpha.example", model: "first" },
    { ref: "beta/second", provider: "beta", vendor: "beta.example", model: "second" },
  ];
  const items = modelMenu(models, [], "beta/second");
  expect(items.map((item) => item.value)).toEqual([
    "beta/second", `__account:${accountKey("alpha.example")}`, "alpha/first", "__manage-models",
  ]);
  expect(modelMenu(models, [], "alpha/first").map((item) => item.value)).toEqual([
    "alpha/first", `__account:${accountKey("beta.example")}`, "beta/second", "__manage-models",
  ]);
});

it("keeps the normal menu when the current model is absent from the catalog", () => {
  const models: ModelEntry[] = [{ ref: "alpha/first", provider: "alpha", model: "first" }];
  expect(modelMenu(models, [], "removed/model")).toEqual(modelMenu(models));
  expect(modelMenu([], [], "removed/model")).toEqual(modelMenu([]));
});

it("carries the declared vendor mark onto the account heading and its rows, never guessing one", () => {
  const items = modelMenu([
    { ref: "ds/chat", provider: "ds", vendor: "api.deepseek.com", model: "chat", mark: "deepseek" },
    { ref: "deepseek-lookalike/x", provider: "deepseek-lookalike", vendor: "lookalike.example", model: "x" },
  ]);
  const ds = items.find((it) => it.value === "ds/chat");
  const plain = items.find((it) => it.value === "deepseek-lookalike/x");
  expect(items.find((it) => it.header && it.right === "api.deepseek.com")?.mark).toBe("deepseek");
  expect(ds?.mark).toBe("deepseek");
  expect(plain?.mark).toBeUndefined();
});

it("puts a relay's own mark on its heading and leaves its model rows to the letter chip", () => {
  const items = modelMenu([{ ref: "or/x", provider: "or", vendor: "openrouter.ai", model: "x", serviceMark: "openrouter" }]);
  expect(items.find((it) => it.header)?.mark).toBe("openrouter");
  expect(items.find((it) => it.value === "or/x")?.mark).toBeUndefined();
});
