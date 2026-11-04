// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it } from "vitest";
import { cleanup, render } from "@testing-library/react";
import "./testkit";
import { Transcript } from "./Transcript";
import { boot, STORAGE } from "../i18n";
import type { Item } from "../state/session";

beforeEach(() => {
  localStorage.setItem(STORAGE, "zh");
  boot();
});
afterEach(cleanup);

const noop = async () => undefined as never;
const read = (id: string, path: string) => ({ id, name: "read_file", args: JSON.stringify({ path }), output: "x\n", readOnly: true });

it("draws each read of a merged run as its own row in the execution group, as the boards draw them", () => {
  const items: Item[] = [
    { t: "user", id: "u", text: "go" },
    { t: "say", id: "s", text: "looking", done: true },
    { t: "reads", id: "r", tools: [read("c1", "scripts/export.py"), read("c2", "tests/fixtures/sales.csv")] },
  ];
  render(<Transcript items={items} entering={[]} onEntered={() => {}} revision={1} waiting={{}} scroll={{ current: null }} hidden={false} focus={null}
    onApprove={noop} onFullAccess={noop} onPlan={noop} onAnswer={noop} onForget={noop} onExtInvoke={() => {}} onExtSubmit={noop}
    checkpoints={new Map()} onPrepareRewind={noop} onCommitRewind={noop} onPrepareFileRevert={noop} onCommitFileRevert={noop}
    needsProject={false} onOpenProject={() => {}} onKeepHere={() => {}} />);
  const rows = [...document.querySelectorAll(".activity-body .hl")].map((row) => row.querySelector(".arg")?.textContent);
  expect(rows).toEqual(["scripts/export.py", "tests/fixtures/sales.csv"]);
  expect(document.querySelector(".reads-disclosure")).toBeNull();
  expect(document.querySelector(".activity-count")?.textContent).toBe("2 项操作");
});
