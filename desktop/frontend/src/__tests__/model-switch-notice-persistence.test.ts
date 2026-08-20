// Run: tsx src/__tests__/model-switch-notice-persistence.test.ts

import { initialState, reducer } from "../lib/useController";
import { getLocale } from "../lib/i18n";

getLocale();

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

function noticeIds(items: readonly { kind: string; id: string }[]): string[] {
  return items.filter((item) => item.kind === "notice").map((item) => item.id);
}

const replace = (items: readonly { kind: string; id: string }[]) =>
  reducer(initialState, {
    type: "history_replace",
    items: [...items] as never,
    startTurn: 0,
    totalTurns: 1,
    hasOlder: false,
  } as never);

console.log("\nmodel-switch notice survives history replaces");

{
  let s = initialState;
  s = reducer(s, { type: "user", text: "hello", submissionId: "s1" });
  s = reducer(s, {
    type: "local_notice",
    level: "info",
    text: "已切换模型，将在下一轮对话中生效。",
    variant: "model-switch",
    preserveRuntime: true,
  });
  // The turn settles, then the backend history (which never stores local
  // notices) replaces the transcript — the confirmation must not vanish.
  s = reducer(s, { type: "event", e: { kind: "turn_done", tabId: "t", err: false } as never });
  const backendHistory = [{ kind: "user", id: "u0", text: "hello" }];
  s = reducer(s, { type: "history_replace", items: backendHistory as never, startTurn: 0, totalTurns: 1, hasOlder: false } as never);
  eq(noticeIds(s.items).length, 1, "history replace keeps the session's model-switch notice");
  const userIndex = s.items.findIndex((item) => item.kind === "user");
  const noticeIndex = s.items.findIndex((item) => item.kind === "notice");
  eq(noticeIndex > userIndex, true, "model-switch notice sits after the newest user message");
  // A second replace must not duplicate the notice.
  s = reducer(s, { type: "history_replace", items: backendHistory as never, startTurn: 0, totalTurns: 1, hasOlder: false } as never);
  eq(noticeIds(s.items).length, 1, "repeated history replaces do not duplicate the notice");
}

{
  // Without a notice, history replace is unchanged.
  const s = replace([{ kind: "user", id: "u0", text: "hello" }]);
  eq(noticeIds(s.items).length, 0, "history replace without a local notice stays clean");
}

{
  // Notices carry the originating user id and stay with that turn instead of
  // migrating to the newest message.
  let s = initialState;
  s = reducer(s, { type: "user", text: "first", submissionId: "s1" });
  s = reducer(s, { type: "local_notice", level: "info", text: "notice A", variant: "model-switch", turnUserID: "u0" });
  s = reducer(s, { type: "user", text: "second", submissionId: "s2" });
  s = reducer(s, { type: "local_notice", level: "info", text: "notice B", variant: "model-switch", turnUserID: "u1" });
  s = reducer(s, {
    type: "history_replace",
    items: [
      { kind: "user", id: "u0", text: "first" },
      { kind: "user", id: "u1", text: "second" },
    ] as never,
    startTurn: 0,
    totalTurns: 2,
    hasOlder: false,
  } as never);
  const order = s.items.map((item) => (item.kind === "notice" ? `notice:${item.text}` : `${item.kind}:${item.text}`));
  eq(
    order.join(","),
    "user:first,notice:notice A,user:second,notice:notice B",
    "each model-switch notice stays with its originating turn",
  );
}

if (failed > 0) {
  console.error(`\n${failed} model-switch notice persistence test(s) failed; ${passed} passed.`);
  process.exit(1);
}
console.log(`\n${passed} model-switch notice persistence tests passed.`);
