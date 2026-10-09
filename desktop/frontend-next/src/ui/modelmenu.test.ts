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
