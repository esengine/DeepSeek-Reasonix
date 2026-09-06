// Run: tsx src/__tests__/ask-submit.test.ts

import type { AppBindings } from "../lib/bridge";
import { answerPromptForActiveTurn } from "../lib/inboxSubmit";
import type { QuestionAnswer, TabMeta } from "../lib/types";

let passed = 0;
let failed = 0;

function eq(actual: unknown, expected: unknown, label: string) {
  if (actual === expected) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}\n`);
    failed += 1;
  }
}

function binding(overrides: Partial<AppBindings>): AppBindings {
  return overrides as AppBindings;
}

const answers: QuestionAnswer[] = [{ questionId: "q1", selected: ["yes"] }];
const tab = { id: "tab-ask", turnId: "turn-authoritative" } as TabMeta;

console.log("\nAsk prompt active-turn submission");

{
  let listCalls = 0;
  const exactCalls: string[] = [];
  await answerPromptForActiveTurn(binding({
    ListTabs: async () => { listCalls += 1; return [tab]; },
    ResolvePromptForTab: async (tabId, promptId, turnId) => { exactCalls.push(`${tabId}:${turnId}:${promptId}`); },
  }), "tab-ask", "ask-1", answers, "turn-known");
  eq(listCalls, 0, "card identity avoids an unnecessary ListTabs lookup");
  eq(exactCalls.join("|"), "tab-ask:turn-known:ask-1", "card turn id fences the exact answer");
}

{
  let listCalls = 0;
  const exactCalls: string[] = [];
  await answerPromptForActiveTurn(binding({
    ListTabs: async () => { listCalls += 1; return [tab]; },
    ResolvePromptForTab: async (tabId, promptId, turnId) => { exactCalls.push(`${tabId}:${turnId}:${promptId}`); },
  }), "tab-ask", "ask-2", answers);
  eq(listCalls, 1, "missing local turn id is resolved from ListTabs once");
  eq(exactCalls.join("|"), "tab-ask:turn-authoritative:ask-2", "resolved turn id fences the exact answer");
}

{
  let exactCalls = 0;
  let rejected = "";
  try {
    await answerPromptForActiveTurn(binding({
      ListTabs: async () => [],
      ResolvePromptForTab: async () => { exactCalls += 1; },
    }), "tab-ask", "ask-3", answers);
  } catch (error) {
    rejected = error instanceof Error ? error.message : String(error);
  }
  eq(rejected.includes("active turn identity is unavailable"), true, "missing authoritative turn id rejects visibly");
  eq(exactCalls, 0, "missing turn id never calls the exact endpoint with an empty fence");
}

{
  let exactCalls = 0;
  let rejected = "";
  try {
    await answerPromptForActiveTurn(binding({
      ListTabs: async () => { throw new Error("ListTabs failed"); },
      ResolvePromptForTab: async () => { exactCalls += 1; },
    }), "tab-ask", "ask-rpc", answers);
  } catch (error) {
    rejected = error instanceof Error ? error.message : String(error);
  }
  eq(rejected, "ListTabs failed", "authoritative turn lookup failure propagates");
  eq(exactCalls, 0, "failed turn lookup never calls the exact endpoint");
}

{
  let rejected = "";
  try {
    await answerPromptForActiveTurn(binding({
      ListTabs: async () => [tab],
      ResolvePromptForTab: async () => { throw new Error("stale turn"); },
    }), "tab-ask", "ask-4", answers);
  } catch (error) {
    rejected = error instanceof Error ? error.message : String(error);
  }
  eq(rejected, "stale turn", "exact endpoint rejection propagates to the decision surface");
}

{
  let rejected = "";
  try {
    await answerPromptForActiveTurn(binding({ ListTabs: async () => [tab] }), "tab-ask", "ask-legacy", answers);
  } catch (error) {
    rejected = error instanceof Error ? error.message : String(error);
  }
  eq(rejected.includes("active turn identity is unavailable"), true, "missing exact prompt API fails closed");
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
