import {
  db,
  getStyles,
  renderer
} from "./chunk-LD7AWGHJ.mjs";
import {
  populateCommonDb
} from "./chunk-6ZKBGPIT.mjs";
import {
  MermaidParseError
} from "./chunk-BQKPLVCE.mjs";
import "./chunk-NYCIP5HP.mjs";
import "./chunk-6QIBY7DQ.mjs";
import "./chunk-SLCUJWJ3.mjs";
import "./chunk-43ACQNTO.mjs";
import "./chunk-FXRKSSP6.mjs";
import {
  createRailroadAbnfServices
} from "./chunk-RRYK4PFG.mjs";
import "./chunk-IT6C5QXO.mjs";
import "./chunk-GMJQP6DO.mjs";
import "./chunk-ODPFZSHR.mjs";
import "./chunk-6OZ7KPF7.mjs";
import "./chunk-LJTN6OYE.mjs";
import "./chunk-Z6Q54H3S.mjs";
import "./chunk-4KMQCWFH.mjs";
import "./chunk-S7Q6ZHN2.mjs";
import "./chunk-IIWGMRJM.mjs";
import "./chunk-A7G5T7E5.mjs";
import "./chunk-PJCWW7M6.mjs";
import "./chunk-CHAKFXHA.mjs";
import {
  log
} from "./chunk-FO5PYUIK.mjs";
import {
  __name
} from "./chunk-PTVI3W5X.mjs";

// src/diagrams/railroad/parser/abnfParser.ts
var langiumParser = createRailroadAbnfServices().RailroadAbnf.parser.LangiumParser;
var transformAlternation = /* @__PURE__ */ __name((alt) => {
  const alternatives = alt.alternatives.map(transformConcatenation);
  if (alternatives.length === 1) {
    return alternatives[0];
  }
  return {
    type: "choice",
    alternatives
  };
}, "transformAlternation");
var transformConcatenation = /* @__PURE__ */ __name((concat) => {
  const elements = concat.elements.map(transformElement);
  if (elements.length === 1) {
    return elements[0];
  }
  return {
    type: "sequence",
    elements
  };
}, "transformConcatenation");
var parseRepeat = /* @__PURE__ */ __name((repeat) => {
  if (repeat.includes("*")) {
    const [minStr, maxStr] = repeat.split("*");
    const min = minStr ? parseInt(minStr, 10) : 0;
    const max = maxStr ? parseInt(maxStr, 10) : Infinity;
    return { min, max };
  }
  const exact = parseInt(repeat, 10);
  return { min: exact, max: exact };
}, "parseRepeat");
var transformElement = /* @__PURE__ */ __name((element) => {
  const inner = transformPrimary(element.primary);
  if (!element.repeat) {
    return inner;
  }
  const { min, max } = parseRepeat(element.repeat);
  if (min === 0 && max === 1) {
    return { type: "optional", element: inner };
  }
  return {
    type: "repetition",
    element: inner,
    min,
    max
  };
}, "transformElement");
var transformPrimary = /* @__PURE__ */ __name((primary) => {
  switch (primary.$type) {
    case "AbnfStringLiteral":
      return {
        type: "terminal",
        value: primary.value
      };
    case "AbnfNumVal":
      return {
        type: "terminal",
        value: primary.value
      };
    case "AbnfRuleName":
      return {
        type: "nonterminal",
        name: primary.name
      };
    case "AbnfGroup":
      return transformAlternation(primary.element);
    case "AbnfOptionalGroup":
      return {
        type: "optional",
        element: transformAlternation(primary.element)
      };
    default:
      throw new Error(`Unsupported ABNF primary node: ${primary.$type}`);
  }
}, "transformPrimary");
var transformRule = /* @__PURE__ */ __name((rule) => {
  return {
    name: rule.name,
    definition: transformAlternation(rule.definition)
  };
}, "transformRule");
var populateDb = /* @__PURE__ */ __name((ast) => {
  populateCommonDb(ast, db);
  if (ast.title) {
    db.setTitle(ast.title);
  }
  ast.rules.map((rule) => db.addRule(transformRule(rule)));
}, "populateDb");
var parser = {
  parse: /* @__PURE__ */ __name((input) => {
    db.clear();
    log.debug("[ABNF Parser] Starting Langium parse");
    const result = langiumParser.parse(input);
    if (result.lexerErrors.length > 0 || result.parserErrors.length > 0) {
      throw new MermaidParseError(result);
    }
    const ast = result.value;
    log.debug("[ABNF Parser] Parsed rules:", ast.rules.length);
    populateDb(ast);
    log.debug("[ABNF Parser] Parse complete");
  }, "parse"),
  parser: {
    yy: db
  }
};

// src/diagrams/railroad/abnfDiagram.ts
var diagram = {
  parser,
  db,
  renderer,
  styles: getStyles
};
export {
  diagram
};
