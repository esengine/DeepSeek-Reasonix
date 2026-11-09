import { renderToStaticMarkup } from "react-dom/server";
import { expect, it } from "vitest";
import { Icon } from "./memicons";

it.each(["unknown-kind", "constructor", "toString", "__proto__"])("uses a bookmark for the unknown memory kind %s", (name) => {
  expect(renderToStaticMarkup(<Icon name={name} />)).toContain("lucide-bookmark");
});
