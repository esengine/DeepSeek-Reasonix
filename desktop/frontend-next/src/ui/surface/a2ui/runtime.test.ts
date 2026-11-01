// @vitest-environment jsdom
import { expect, it } from "vitest";
import { createSurfaceRuntime } from "./runtime";

it("maps field ids to safe bindings and returns typed values from the library data model", () => {
  const runtime = createSurfaceRuntime([
    { kind: "editable_text", key: "__proto__", label: "Draft", field: { type: "string", required: true, default: "  initial\n" } },
    { kind: "field", key: "constructor", label: "Count", field: { type: "number", default: 0 } },
    { kind: "field", key: "toString", label: "Enabled", field: { type: "bool", default: false } },
  ]);
  try {
    const draft = "  edited\n\n@file <script>\t ";
    runtime.surface.dataModel.set("/fields/f0", draft);
    runtime.surface.dataModel.set("/fields/f1", "3");
    const answer = runtime.answers();
    expect(Object.getPrototypeOf(answer.values)).toBeNull();
    expect(JSON.parse(JSON.stringify(answer.values))).toEqual({ ["__proto__"]: draft, constructor: 3, toString: false });
    expect(answer.valid).toBe(true);
    runtime.surface.dataModel.set("/fields/f1", "");
    expect(Object.hasOwn(runtime.answers().values, "constructor")).toBe(false);
    runtime.setAnswers({ ["__proto__"]: "restored", constructor: 2, toString: true });
    expect(runtime.answers().values["__proto__"]).toBe("restored");
  } finally { runtime.dispose(); }
});

it("does not expose executable components or library functions through the Studio catalogue", () => {
  const runtime = createSurfaceRuntime([{ kind: "text", value: '<script>bad</script> {"path":"/secret"}' }]);
  try {
    const catalog = runtime.surface.defaultCatalog;
    expect(catalog?.functions.size).toBe(0);
    expect(catalog?.components.has("Image")).toBe(false);
    expect(catalog?.components.has("Video")).toBe(false);
    expect(catalog?.components.has("Modal")).toBe(false);
  } finally { runtime.dispose(); }
});
