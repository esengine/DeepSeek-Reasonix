import assert from "node:assert/strict";
import { test } from "node:test";
import { renderTable, selectTop } from "./update-acknowledgments.mjs";
import { thanksExclusions } from "./release-credits.mjs";

const person = (login, contributions) => ({ login, type: "User", contributions, html_url: `https://github.com/${login}` });

function fixture() {
  const rows = [
    person("esengine", 900),
    { type: "Anonymous", name: "yhh", email: "", contributions: 800 },
    { type: "Anonymous", name: "reasonix", email: "", contributions: 700 },
    { type: "Anonymous", name: "merge-order-check", email: "", contributions: 600 },
    { login: "dependabot[bot]", type: "Bot", contributions: 500, html_url: "https://github.com/apps/dependabot" },
    { type: "Anonymous", name: "Some Person", email: "", contributions: 450 },
  ];
  for (let i = 0; i < 24; i += 1) rows.push(person(`user${String(i).padStart(2, "0")}`, 400 - i));
  rows.push(person("zed", 400 - 23), person("amy", 400 - 23));
  return rows;
}

const excluded = thanksExclusions("esengine/DeepSeek-Reasonix", {});

test("owner, alias, tool identity, bot and unlinked anonymous entries are left out", () => {
  const top = selectTop(fixture(), excluded);
  const labels = top.map((row) => row.login);
  for (const gone of ["esengine", "yhh", "reasonix", "merge-order-check", "dependabot[bot]", "Some Person"]) {
    assert.ok(!labels.includes(gone), gone);
  }
  assert.ok(top.every((row) => row.login && row.url));
});

test("the cap applies after filtering, so the table stays 20 long", () => {
  const top = selectTop(fixture(), excluded);
  assert.equal(top.length, 20);
  assert.deepEqual(top.map((row) => row.rank), Array.from({ length: 20 }, (_, i) => i + 1));
  assert.equal(top[0].login, "user00");
});

test("order is by commits, ties by login, whatever order the API answered in", () => {
  const rows = [person("b", 5), person("a", 5), person("top", 9)];
  assert.deepEqual(selectTop(rows, excluded).map((row) => row.login), ["top", "a", "b"]);
  assert.deepEqual(selectTop([...rows].reverse(), excluded), selectTop(rows, excluded));
});

test("a declared name is excluded case-insensitively, as a login too", () => {
  const rows = [person("ESENGINE", 9), person("Reasonix", 8), person("ok", 1)];
  assert.deepEqual(selectTop(rows, excluded).map((row) => row.login), ["ok"]);
});

test("rendering is idempotent and a pure function of the rows", () => {
  const table = renderTable(selectTop(fixture(), excluded));
  assert.equal(table, renderTable(selectTop(fixture(), excluded)));
  assert.equal(table.split("\n").length, 2 + 5 + 2);
});
