import assert from "node:assert/strict";
import { test } from "node:test";
import { loginsFrom, ownerOf, render } from "./update-contributors.mjs";

test("bots are dropped and the rest sorted by contributions, ties by login", () => {
  const rows = [
    { login: "b", type: "User", contributions: 3 },
    { login: "dependabot[bot]", type: "Bot", contributions: 99 },
    { login: "a", type: "User", contributions: 3 },
    { login: "top", type: "User", contributions: 10 },
    { login: "github-actions", type: "User", contributions: 50 },
  ];
  assert.deepEqual(loginsFrom(rows), ["top", "a", "b"]);
});

test("the file is a stable JSON array ending in a newline", () => {
  assert.equal(render(["a", "b"]), '[\n  "a",\n  "b"\n]\n');
});

test("the repository owner is left out, whatever the case, and nobody else moves", () => {
  const rows = [
    { login: "Owner", type: "User", contributions: 100 },
    { login: "b", type: "User", contributions: 3 },
    { login: "a", type: "User", contributions: 3 },
  ];
  assert.equal(ownerOf("esengine/DeepSeek-Reasonix"), "esengine");
  assert.deepEqual(loginsFrom(rows, "owner"), ["a", "b"]);
  assert.deepEqual(loginsFrom(rows, "owner"), loginsFrom([...rows].reverse(), "owner"));
  assert.deepEqual(loginsFrom(rows), ["Owner", "a", "b"]);
});
