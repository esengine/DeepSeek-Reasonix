import { describe, expect, it } from "vitest";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const SHARED = ["m-fade", "m-card", "m-panel", "m-exit", "m-spin", "m-pulse"];

const REGISTERED: Record<string, string> = {
  "step-in": "Think-Exec", "step-pop": "Think-Exec", "step-shake": "Think-Exec",
  "activity-shimmer": "Think-Exec", "think-sweep": "Think-Exec", "bump-up": "Think-Exec", "caret-blink": "Think-Exec",
  "demo-in": "Onb-Motion", "demo-gone": "Onb-Motion", "demo-out": "Onb-Motion", "demo-draw": "Onb-Motion",
  "onb-mark-in": "Onb-Motion", "onb-rise": "Onb-Motion", "onb-breathe": "Onb-Motion", "onb-grid": "Onb-Motion", "onb-banner-in": "Onb-Motion", "onb-dash": "Onb-Motion", "onb-land": "Onb-Motion", "onb-leave": "Onb-Motion", "compose-rise": "Onb-Motion",
  "onb-step-in": "Onb-Motion", "onb-step-back": "Onb-Motion", "onb-tile": "Onb-Motion", "onb-shake": "Onb-Motion",
  oobebrand: "Onb-Flow", oobecollapse: "Onb-Flow", oobefill: "Onb-Flow", oobegone: "Onb-Flow", oobelume: "Onb-Flow", oobeout: "Onb-Flow", oobesay: "Onb-Flow", oobeshrink: "Onb-Flow",
  "plan-spin-stem": "Chat-Plan", "plan-spin-bowl": "Chat-Plan", "plan-spin-leg": "Chat-Plan", "plan-open": "Chat-Plan", "plan-close": "Chat-Plan",
  "es-flow": "Chat-Composer", "sheet-up": "Chat-Composer", "ph-run": "Run-Phases",
  "workbench-tab-in": "WB-Frame", "workbench-live": "WB-Frame",
  "srail-flash": "Sess-States", "srail-peek": "Sess-States", rowsweep: "Sess-States", "agent-live": "Subagent",
  "sel-tools-in": "Chat-Reply", "sel-tools-under": "Chat-Reply",
  "compose-sweep": "Chat-Composer", sendsheen: "Chat-Composer", chromesweep: "Chat-Composer",
  "studio-io-up": "Chat-Composer", "studio-io-down": "Chat-Composer", "studio-io-tick": "Chat-Composer",
  "studio-r-run": "Run-Phases", trace: "Run-Phases", symdone: "Run-Phases",
  "studio-ambient-drift": "Spec-Color", drift1: "Spec-Color", drift2: "Spec-Color", drift3: "Spec-Color",
  flash: "Chat-Reply", landed: "Chat-Reply", hitmark: "Chat-Reply", hitglyph: "Chat-Reply", edgebump: "Msg-Rail", nudge: "Msg-Rail", bob: "Msg-Rail", chipfollow: "Msg-Rail",
};

function cssFiles(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) return name === "node_modules" ? [] : cssFiles(path);
    return name.endsWith(".css") ? [path] : [];
  });
}

const src = fileURLToPath(new URL("..", import.meta.url));
const defined = new Set(cssFiles(src).flatMap((file) => [...readFileSync(file, "utf8").matchAll(/@keyframes\s+([\w-]+)/g)].map((m) => m[1]!)));

describe("keyframes", () => {
  it("are the six shared ones plus the ones a board draws", () => {
    const stray = [...defined].filter((name) => !SHARED.includes(name) && !(name in REGISTERED));
    expect(stray, "an unregistered keyframe: reuse a shared one or register it with its board").toEqual([]);
  });

  it("registers nothing that no stylesheet defines", () => {
    expect(Object.keys(REGISTERED).filter((name) => !defined.has(name))).toEqual([]);
  });

  it("defines each shared keyframe", () => {
    expect(SHARED.filter((name) => !defined.has(name))).toEqual([]);
  });
});
