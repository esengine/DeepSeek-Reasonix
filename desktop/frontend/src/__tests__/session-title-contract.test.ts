// Run: tsx src/__tests__/session-title-contract.test.ts

import {
  historySessionDisplayTitle,
  paletteSessionDisplayTitle,
  paletteSessionHint,
  paletteSessionKeywords,
} from "../lib/session";
import type { SessionMeta } from "../lib/types";

let passed = 0;
let failed = 0;

function eq<T>(actual: T, expected: T, label: string) {
  if (Object.is(actual, expected)) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n    expected: ${String(expected)}\n    actual:   ${String(actual)}\n`);
    failed += 1;
  }
}

function deepEq<T>(actual: T, expected: T, label: string) {
  const actualJson = JSON.stringify(actual);
  const expectedJson = JSON.stringify(expected);
  if (actualJson === expectedJson) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n    expected: ${expectedJson}\n    actual:   ${actualJson}\n`);
    failed += 1;
  }
}

function session(overrides: Partial<SessionMeta> = {}): SessionMeta {
  return {
    path: "/sessions/topic-backed.jsonl",
    preview: "first saved prompt preview",
    title: "Renamed saved session",
    turns: 3,
    createdAt: 100,
    lastActivityAt: 200,
    modTime: 200,
    current: false,
    open: false,
    workspaceRoot: "/work/project-alpha",
    topicId: "topic-1",
    topicTitle: "Shared topic",
    ...overrides,
  };
}

console.log("\nsession title contracts");

{
  const item = session();
  eq(
    paletteSessionDisplayTitle(item, "Untitled"),
    "Shared topic",
    "Cmd+K displays the topic title for topic-backed sessions",
  );
  deepEq(
    paletteSessionKeywords(item),
    ["Renamed saved session", "first saved prompt preview"],
    "Cmd+K still searches the session rename and preview text",
  );
  eq(
    paletteSessionHint(item),
    "Renamed saved session · /work/project-alpha",
    "Cmd+K shows the session rename in the hint so hidden search hits are visible",
  );
}

{
  const item = session();
  eq(
    historySessionDisplayTitle(item, "Untitled"),
    "Renamed saved session",
    "History displays an explicit session rename before the topic title",
  );
}

{
  const item = session({ title: "  " });
  eq(
    historySessionDisplayTitle(item, "Untitled"),
    "Shared topic",
    "Unrenamed topic-backed history sessions fall back to topic title",
  );
  deepEq(
    paletteSessionKeywords(item),
    ["first saved prompt preview"],
    "Blank session titles are not added as Cmd+K keywords",
  );
  eq(
    paletteSessionHint(item),
    "first saved prompt preview · /work/project-alpha",
    "Cmd+K falls back to preview text in the hint when there is no custom session title",
  );
}

{
  const item = session({ title: "Shared topic", preview: "first saved prompt preview" });
  eq(
    paletteSessionHint(item),
    "first saved prompt preview · /work/project-alpha",
    "Cmd+K skips duplicate title hints that repeat the visible topic title",
  );
}

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
