import { describe, expect, it } from "vitest";
import { t } from "../i18n";
import { cheapestEffort, effortMenu, forcesThinkingFor, routeEffortPick } from "./effort";
import type { ModelEntry } from "../port/model";

describe("a ladder that carries every OpenAI rung", () => {
  const ladder = ["auto", "none", "low", "medium", "high", "xhigh", "max"];
  const rows = effortMenu(ladder, "gpt-5.6-sol", "__declare").filter((row) => ladder.includes(row.value));

  it("names each rung once", () => {
    const labels = rows.map((row) => row.label);
    expect(new Set(labels).size).toBe(labels.length);
  });

  it("shows no reasoning as an empty meter, not as the shallowest depth", () => {
    const none = rows.find((row) => row.value === "none");
    expect(none && "strength" in none ? none.strength : undefined).toBe(0);
  });
});

describe("a mode row in the effort menu", () => {
  const pro = { id: "pro", labelKey: "model_mode.pro", hintKey: "model_mode.pro.hint", costlier: true, active: false };

  it("sits after the ladder and before the footnote, as a switch", () => {
    const rows = effortMenu(["auto", "high"], "gpt-5.6-sol", "__declare", [pro]);
    const at = rows.findIndex((row) => row.value === "__mode:pro");
    expect(at).toBe(rows.length - 2);
    expect(rows[at]).toMatchObject({ toggle: false, divide: true });
  });

  it("is absent when the model declares none", () => {
    expect(effortMenu(["auto", "high"], "qwen3.8-max", "__declare").some((row) => row.value.startsWith("__mode:"))).toBe(false);
  });

  it("routes a pick to the mode, the declaration or the level it names", () => {
    const calls: string[] = [];
    const act = { declare: () => calls.push("declare"), effort: (l: string) => calls.push(`effort:${l}`), mode: (m: string) => calls.push(`mode:${m}`) };
    routeEffortPick("__mode:pro", [pro], act);
    routeEffortPick("__mode:pro", [{ ...pro, active: true }], act);
    routeEffortPick("__mode:gone", [pro], act);
    routeEffortPick("__effort-declare", [pro], act);
    routeEffortPick("high", [pro], act);
    expect(calls).toEqual(["mode:pro", "mode:", "declare", "effort:high"]);
  });
});

describe("a model that cannot switch thinking off", () => {
  const ladder = ["auto", "low", "high", "max"];
  const desc = (rows: ReturnType<typeof effortMenu>, value: string) => rows.find((row) => row.value === value)?.desc;
  const plain = effortMenu(ladder, "glm-5.3", "__declare");
  const forced = effortMenu(ladder, "glm-5.3", "__declare", [], true);

  it("says so on its cheapest level, which is where a saved off lands", () => {
    expect(desc(forced, "low")).toContain(t("思考仍开启并计费，该模型无法关闭思考"));
    expect(desc(forced, "low")).not.toBe(desc(plain, "low"));
  });

  it("leaves the other rungs reading as they did", () => {
    for (const value of ["auto", "high", "max"]) {
      expect(desc(forced, value)).toBe(desc(plain, value));
    }
  });

  it("names the cheapest rung as the first one past auto", () => {
    expect(cheapestEffort(ladder)).toBe("low");
    expect(cheapestEffort(["auto"])).toBe("");
  });

  it("reads the flag off the model in hand, and only from a true one", () => {
    const models = [
      { ref: "zai/glm-5.3", provider: "zai", model: "glm-5.3", forcesThinking: true },
      { ref: "zai/glm-5.2", provider: "zai", model: "glm-5.2" },
    ] as ModelEntry[];
    expect(forcesThinkingFor(models, "zai/glm-5.3")).toBe(true);
    expect(forcesThinkingFor(models, "zai/glm-5.2")).toBe(false);
    expect(forcesThinkingFor(models, "gone/ref")).toBe(false);
  });
});
