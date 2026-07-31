const assert = require('node:assert');
const { test } = require('node:test');

const Parser = require('tree-sitter');

test('can load TypeScript grammar', () => {
  const parser = new Parser();
  assert.doesNotThrow(() => parser.setLanguage(require('./typescript')));
});

test('can load TSX grammar', () => {
  const parser = new Parser();
  assert.doesNotThrow(() => parser.setLanguage(require('./tsx')));
});
