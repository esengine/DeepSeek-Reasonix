import { describe, expect, it } from "vitest";
import { EN } from "./en";
import { NOTICE_TEXT } from "./notices";
import { NOTICE_VAR_CODES, leftoverDetail, noticeWording } from "./notice_wording";

const placeholders = (s: string) => [...s.matchAll(/\{(\w+)\}/g)].map((m) => m[1]);

const PAYLOAD: Record<string, string | undefined> = {
  extension_skipped: JSON.stringify({ extension: "e", point: "tool.before" }),
  inbox_recovered: JSON.stringify({ count: 2 }),
  permission_rules_dormant: JSON.stringify({ rules: [{ list: "ask", rule: "Foo(bar)" }] }),
  job_started: JSON.stringify({ id: "j", label: "build" }),
  job_finished: JSON.stringify({ id: "j", label: "build" }),
  job_killed: JSON.stringify({ id: "j", label: "build" }),
  job_failed: JSON.stringify({ id: "j", label: "build" }),
  job_stalled: JSON.stringify({ id: "j", label: "build", quietSeconds: 900 }),
  compaction_telemetry: JSON.stringify({ mode: "summarized", source_tokens: 122525, projection_tokens: 61192, input_tokens: 40710, output_tokens: 8186, cache_hit_tokens: 256, request_count: 1 }),
  context_composition: JSON.stringify({ total: 561061, at: 419430, active: 400000, parts: [{ name: "tool_output", tokens: 500000 }] }),
  subagent_waiting: JSON.stringify({ task: "fix the typo", by: "rewrite the docs", whole: true }),
  display_currency: "USD",
  compact_declined: undefined,
  compact_failed: "summary_failed",
  compact_held: "summary_failed",
};

describe("notice wording", () => {
  it.each(Object.entries(NOTICE_TEXT))("%s reads in English too", (code, sentence) => {
    const english = EN[sentence];
    expect(english, `${code}: no English for "${sentence}" - an English window would show the Chinese`).toBeTruthy();
    expect(/[\u4e00-\u9fff]/.test(english), `${code}: the English still holds Chinese`).toBe(false);
  });

  const withSlots = Object.entries(NOTICE_TEXT).filter(([, s]) => placeholders(s).length > 0);

  it.each(withSlots)("%s registers a reader for every placeholder it has", (code, sentence) => {
    expect(NOTICE_VAR_CODES, `${code} has placeholders but no variable reader in notice_wording.ts`).toContain(code);
    expect(code in PAYLOAD, `add a sample payload for ${code}`).toBe(true);
    const w = noticeWording(code, PAYLOAD[code]);
    expect(w).toBeDefined();
    for (const key of placeholders(sentence)) expect(w?.vars).toHaveProperty(key);
  });

  it.each(NOTICE_VAR_CODES)("%s yields nothing, never a half-filled sentence, for an unreadable detail", (code) => {
    if (code === "display_currency" || code === "compact_declined") return;
    expect(noticeWording(code, "not json")).toBeUndefined();
  });

  it("words a placeholder-free code without variables", () => {
    expect(noticeWording("verification_stalled", "anything")?.vars).toBeUndefined();
  });

  it("has no wording for an unknown code", () => {
    expect(noticeWording("some_future_code", "x")).toBeUndefined();
    expect(noticeWording(undefined, "x")).toBeUndefined();
  });

  it.each([
    ["extension_skipped", '{"extension":"","point":"p"}'],
    ["extension_skipped", '{"extension":3,"point":"p"}'],
    ["inbox_recovered", '{"count":"2"}'],
    ["inbox_recovered", '{"count":0}'],
    ["inbox_recovered", '{"count":1.5}'],
    ["permission_rules_dormant", '{"rules":[]}'],
    ["permission_rules_dormant", '{"rules":[{"list":"ask"}]}'],
    ["permission_rules_dormant", '{"rules":[{"list":1,"rule":"x"}]}'],
    ["job_finished", '{"label":"build"}'],
    ["job_killed", '{"id":""}'],
    ["job_failed", '{"id":5}'],
    ["job_stalled", '{"id":"j"}'],
    ["job_stalled", '{"id":"j","quietSeconds":"900"}'],
    ["job_stalled", '{"id":"j","quietSeconds":0}'],
    ["compaction_telemetry", '{"mode":"summarized"}'],
    ["compaction_telemetry", '{"mode":"","source_tokens":1,"projection_tokens":1}'],
    ["compaction_telemetry", '{"mode":"summarized","source_tokens":"122525","projection_tokens":1}'],
    ["compact_failed", "no_such_reason"],
  ])("%s reads valid JSON of the wrong shape (%s) as no payload", (code, detail) => {
    expect(noticeWording(code, detail)).toBeUndefined();
  });

  it("keeps the diagnostic only where the detail was not read as a payload", () => {
    expect(leftoverDetail("job_failed", '{"id":"j","error":"exit 2"}', { name: "j" })).toBe("exit 2");
    expect(leftoverDetail("job_finished", '{"id":"j"}', { name: "j" })).toBeUndefined();
    expect(leftoverDetail("compact_failed", "future_code", undefined)).toBe("future_code");
  });
});
