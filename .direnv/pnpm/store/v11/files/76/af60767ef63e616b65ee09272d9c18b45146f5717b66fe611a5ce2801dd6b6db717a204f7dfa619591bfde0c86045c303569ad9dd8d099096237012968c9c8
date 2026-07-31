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
import "./chunk-RRYK4PFG.mjs";
import {
  createRailroadPegServices
} from "./chunk-IT6C5QXO.mjs";
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

// src/diagrams/railroad/parser/pegParser.ts
var langiumParser = createRailroadPegServices().RailroadPeg.parser.LangiumParser;
var transformOrderedChoice = /* @__PURE__ */ __name((choice) => {
  const alternatives = choice.alternatives.map(transformSequence);
  if (alternatives.length === 1) {
    return alternatives[0];
  }
  return {
    type: "choice",
    alternatives
  };
}, "transformOrderedChoice");
var transformSequence = /* @__PURE__ */ __name((sequence) => {
  const elements = sequence.elements.map(transformPrefix);
  if (elements.length === 1) {
    return elements[0];
  }
  return {
    type: "sequence",
    elements
  };
}, "transformSequence");
var transformPrefix = /* @__PURE__ */ __name((prefix) => {
  const inner = transformSuffix(prefix.suffix);
  if (!prefix.operator) {
    return inner;
  }
  const label = prefix.operator === "&" ? `&${nodeToLabel(inner)}` : `!${nodeToLabel(inner)}`;
  return {
    type: "special",
    text: label
  };
}, "transformPrefix");
var nodeToLabel = /* @__PURE__ */ __name((node) => {
  switch (node.type) {
    case "terminal":
      return `"${node.value}"`;
    case "nonterminal":
      return node.name;
    case "special":
      return node.text;
    default:
      return "(...)";
  }
}, "nodeToLabel");
var transformSuffix = /* @__PURE__ */ __name((suffix) => {
  const inner = transformPrimary(suffix.primary);
  if (!suffix.operator) {
    return inner;
  }
  switch (suffix.operator) {
    case "?":
      return { type: "optional", element: inner };
    case "*":
      return { type: "repetition", element: inner, min: 0, max: Infinity };
    case "+":
      return { type: "repetition", element: inner, min: 1, max: Infinity };
    default:
      throw new Error(`Unsupported PEG suffix operator: ${suffix.operator}`);
  }
}, "transformSuffix");
var transformPrimary = /* @__PURE__ */ __name((primary) => {
  switch (primary.$type) {
    case "PegLiteral":
      return {
        type: "terminal",
        value: primary.value
      };
    case "PegIdentifier":
      return {
        type: "nonterminal",
        name: primary.name
      };
    case "PegGroup":
      return transformOrderedChoice(primary.element);
    case "PegAny":
      return {
        type: "special",
        text: primary.dot
      };
    default:
      throw new Error(`Unsupported PEG primary node: ${primary.$type}`);
  }
}, "transformPrimary");
var transformRule = /* @__PURE__ */ __name((rule) => {
  return {
    name: rule.name,
    definition: transformOrderedChoice(rule.definition)
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
    log.debug("[PEG Parser] Starting Langium parse");
    const result = langiumParser.parse(input);
    if (result.lexerErrors.length > 0 || result.parserErrors.length > 0) {
      throw new MermaidParseError(result);
    }
    const ast = result.value;
    log.debug("[PEG Parser] Parsed rules:", ast.rules.length);
    populateDb(ast);
    log.debug("[PEG Parser] Parse complete");
  }, "parse"),
  parser: {
    yy: db
  }
};

// src/diagrams/railroad/pegDiagram.ts
var diagram = {
  parser,
  db,
  renderer,
  styles: getStyles
};
export {
  diagram
};
