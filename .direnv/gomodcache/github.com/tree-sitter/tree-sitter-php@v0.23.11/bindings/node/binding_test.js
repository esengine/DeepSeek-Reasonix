const assert = require("node:assert");
const { test } = require("node:test");

const Parser = require("tree-sitter");

test("can load PHP grammar", () => {
  const parser = new Parser();
  assert.doesNotThrow(() => parser.setLanguage(require("./php")));
});

test("can load PHP-Only grammar", () => {
  const parser = new Parser();
  assert.doesNotThrow(() => parser.setLanguage(require("./php_only")));
});
