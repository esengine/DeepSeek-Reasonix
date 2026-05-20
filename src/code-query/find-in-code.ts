import type { Node } from "web-tree-sitter";
import { getParser, grammarForPath } from "./parser.js";

export type CodeMatchKind = "call" | "definition" | "reference";

export interface CodeMatch {
  line: number;
  column: number;
  kind: CodeMatchKind;
  snippet: string;
}

export interface FindInCodeOptions {
  kind?: CodeMatchKind | "any";
}

const IDENTIFIER_TYPES = new Set([
  "identifier",
  "property_identifier",
  "type_identifier",
  "shorthand_property_identifier",
  "shorthand_property_identifier_pattern",
]);

const DECLARATION_NAME_PARENTS = new Set([
  "function_declaration",
  "function_signature",
  "class_declaration",
  "interface_declaration",
  "type_alias_declaration",
  "enum_declaration",
  "method_definition",
  "method_signature",
  "abstract_method_signature",
  "public_field_definition",
  "field_definition",
  "property_signature",
  "internal_module",
  "variable_declarator",
]);

export async function findInCode(
  filePath: string,
  source: string,
  name: string,
  opts: FindInCodeOptions = {},
): Promise<CodeMatch[]> {
  if (!name) return [];
  const grammar = grammarForPath(filePath);
  if (!grammar) return [];
  const parser = await getParser(grammar);
  try {
    const tree = parser.parse(source);
    if (!tree) return [];
    try {
      const sourceLines = source.split(/\r?\n/);
      const matches: CodeMatch[] = [];
      walk(tree.rootNode, (node) => {
        if (!IDENTIFIER_TYPES.has(node.type)) return;
        if (node.text !== name) return;
        const kind = classify(node);
        const filter = opts.kind ?? "any";
        if (filter !== "any" && filter !== kind) return;
        const line = node.startPosition.row + 1;
        const column = node.startPosition.column + 1;
        matches.push({
          line,
          column,
          kind,
          snippet: sourceLines[node.startPosition.row] ?? "",
        });
      });
      return matches;
    } finally {
      tree.delete();
    }
  } finally {
    parser.delete();
  }
}

function classify(node: Node): CodeMatchKind {
  const parent = node.parent;
  if (!parent) return "reference";
  if (DECLARATION_NAME_PARENTS.has(parent.type)) {
    const nameField = parent.childForFieldName("name");
    if (nameField && nameField.id === node.id) return "definition";
  }
  if (parent.type === "call_expression" || parent.type === "new_expression") {
    const constructorField = parent.childForFieldName(
      parent.type === "new_expression" ? "constructor" : "function",
    );
    if (constructorField && constructorField.id === node.id) return "call";
  }
  if (parent.type === "member_expression") {
    const propField = parent.childForFieldName("property");
    if (propField && propField.id === node.id) {
      const grandparent = parent.parent;
      if (
        grandparent &&
        (grandparent.type === "call_expression" || grandparent.type === "new_expression")
      ) {
        const callee = grandparent.childForFieldName(
          grandparent.type === "new_expression" ? "constructor" : "function",
        );
        if (callee && callee.id === parent.id) return "call";
      }
    }
  }
  return "reference";
}

function walk(root: Node, visit: (node: Node) => void): void {
  const cursor = root.walk();
  try {
    let visitedChildren = false;
    while (true) {
      if (!visitedChildren) visit(cursor.currentNode);
      if (!visitedChildren && cursor.gotoFirstChild()) continue;
      if (cursor.gotoNextSibling()) {
        visitedChildren = false;
        continue;
      }
      if (!cursor.gotoParent()) return;
      visitedChildren = true;
    }
  } finally {
    cursor.delete();
  }
}
