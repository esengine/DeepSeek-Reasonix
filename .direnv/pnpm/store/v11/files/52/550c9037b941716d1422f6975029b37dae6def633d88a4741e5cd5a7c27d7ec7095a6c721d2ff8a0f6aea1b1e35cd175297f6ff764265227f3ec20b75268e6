"use strict";

const CSSConditionRuleImpl = require("./CSSConditionRule-impl.js").implementation;
const { serializeGroupingRuleBody } = require("./CSSGroupingRule-impl.js");
const csstree = require("./helpers/patched-csstree.js");

function evaluateSupportsCondition(node) {
  const iter = node.children[Symbol.iterator]();
  const child = iter.next().value;

  // "not" prefix
  if (child.type === "Identifier" && child.name === "not") {
    return !evaluateSupportsItem(iter.next().value);
  }

  let result = evaluateSupportsItem(child);

  // Remaining operator + item pairs
  for (const operator of iter) {
    const item = iter.next().value;
    if (operator.name === "and") {
      result &&= evaluateSupportsItem(item);
    } else if (operator.name === "or") {
      result ||= evaluateSupportsItem(item);
    }
  }

  return result;
}

function evaluateSupportsItem(node) {
  if (node.type === "SupportsDeclaration") {
    const { property } = node.declaration;
    const value = csstree.generate(node.declaration.value);
    return csstree.lexer.matchProperty(property, value).error === null;
  }
  if (node.type === "Condition") {
    return evaluateSupportsCondition(node);
  }
  return false;
}

// Evaluate a @supports condition string against csstree's lexer.
function evaluateSupportsText(conditionText) {
  try {
    const ast = csstree.parse(`@supports ${conditionText} {}`, { context: "atrule", atrule: "supports" });
    return evaluateSupportsCondition(ast.prelude.children.first);
  } catch {
    return false;
  }
}

class CSSSupportsRuleImpl extends CSSConditionRuleImpl {
  get type() {
    return 12; // SUPPORTS_RULE
  }

  get matches() {
    return evaluateSupportsText(this._conditionText);
  }

  get cssText() {
    return `@supports ${this._conditionText} ${serializeGroupingRuleBody(this.cssRules._list)}`;
  }
}

exports.implementation = CSSSupportsRuleImpl;
exports.evaluateSupportsText = evaluateSupportsText;
