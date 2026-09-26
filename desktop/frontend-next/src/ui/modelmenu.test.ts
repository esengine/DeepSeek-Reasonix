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
