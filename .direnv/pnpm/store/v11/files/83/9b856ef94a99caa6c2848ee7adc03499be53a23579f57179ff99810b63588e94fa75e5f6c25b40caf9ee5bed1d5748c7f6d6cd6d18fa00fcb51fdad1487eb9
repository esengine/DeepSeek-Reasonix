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
import {
  createRailroadEbnfServices
} from "./chunk-FXRKSSP6.mjs";
import "./chunk-RRYK4PFG.mjs";
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

// src/diagrams/railroad/parser/ebnfParser.ts
var langiumParser = createRailroadEbnfServices().RailroadEbnf.parser.LangiumParser;
var transformChoice = /* @__PURE__ */ __name((choice) => {
  const alternatives = choice.alternatives.map(transformSequence);
  if (alternatives.length === 1) {
    return alternatives[0];
  }
  return {
    type: "choice",
    alternatives
  };
}, "transformChoice");
var transformSequence = /* @__PURE__ */ __name((sequence) => {
  const elements = sequence.elements.map(transformTerm);
  if (elements.length === 1) {
    return elements[0];
  }
  return {
    type: "sequence",
    elements
  };
}, "transformSequence");
var transformPrimary = /* @__PURE__ */ __name((primary) => {
  switch (primary.$type) {
    case "EbnfTerminal":
      return {
        type: "terminal",
        value: primary.value
      };
    case "EbnfNonTerminal":
      return {
        type: "nonterminal",
        name: primary.name
      };
    case "EbnfSpecial":
      return {
        type: "special",
        text: primary.text
      };
    case "EbnfGroup":
      return transformChoice(primary.element);
    case "EbnfOptional":
      return {
        type: "optional",
        element: transformChoice(primary.element)
      };
    case "EbnfRepetition":
      return {
        type: "repetition",
        element: transformChoice(primary.element),
        min: 0,
        max: Infinity
      };
    default:
      throw new Error(`Unsupported EBNF primary node: ${primary.$type}`);
  }
}, "transformPrimary");
var transformPostfix = /* @__PURE__ */ __name((node, postfix) => {
  switch (postfix.$type) {
    case "EbnfOptionalPostfix":
      return {
        type: "optional",
        element: node
      };
    case "EbnfZeroOrMorePostfix":
      return {
        type: "repetition",
        element: node,
        min: 0,
        max: Infinity
      };
    case "EbnfOneOrMorePostfix":
      return {
        type: "repetition",
        element: node,
        min: 1,
        max: Infinity
      };
    case "EbnfExceptionPostfix":
      return {
        type: "sequence",
        elements: [
          node,
          { type: "terminal", value: "-" },
          transformPrimary(postfix.except)
        ]
      };
    default:
      throw new Error(`Unsupported EBNF postfix node: ${postfix.$type}`);
  }
}, "transformPostfix");
var transformTerm = /* @__PURE__ */ __name((term) => {
  return term.postfixes.reduce((currentNode, postfix) => {
    return transformPostfix(currentNode, postfix);
  }, transformPrimary(term.base));
}, "transformTerm");
var transformRule = /* @__PURE__ */ __name((rule) => {
  return {
    name: rule.name,
    definition: transformChoice(rule.definition)
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
    log.debug("[EBNF Parser] Starting Langium parse");
    const result = langiumParser.parse(input);
    if (result.lexerErrors.length > 0 || result.parserErrors.length > 0) {
      throw new MermaidParseError(result);
    }
    const ast = result.value;
    log.debug("[EBNF Parser] Parsed rules:", ast.rules.length);
    populateDb(ast);
    log.debug("[EBNF Parser] Parse complete");
  }, "parse"),
  parser: {
    yy: db
  }
};

// src/diagrams/railroad/ebnfDiagram.ts
var diagram = {
  parser,
  db,
  renderer,
  styles: getStyles
};
export {
  diagram
};
