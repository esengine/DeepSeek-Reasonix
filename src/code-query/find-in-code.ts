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
  "field_identifier",
  "package_identifier",
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
  "function_definition",
  "class_definition",
  "method_declaration",
  "type_spec",
  "function_item",
  "struct_item",
  "enum_item",
  "trait_item",
  "type_item",
  "mod_item",
  "const_item",
  "static_item",
  "constructor_declaration",
]);

const CALL_PARENT_TYPES = new Set([
  "call_expression",
  "new_expression",
  "call",
  "method_invocation",
  "object_creation_expression",
]);

const MEMBER_PARENT_TYPES = new Set([
  "member_expression",
  "attribute",
  "selector_expression",
  "field_expression",
]);

const CALLEE_FIELDS = ["function", "constructor", "name"] as const;
const MEMBER_NAME_FIELDS = ["property", "field", "attribute", "name"] as const;

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
  if (CALL_PARENT_TYPES.has(parent.type) && fieldMatches(parent, node, CALLEE_FIELDS)) {
    return "call";
  }
  if (MEMBER_PARENT_TYPES.has(parent.type) && fieldMatches(parent, node, MEMBER_NAME_FIELDS)) {
    const grandparent = parent.parent;
    if (
      grandparent &&
      CALL_PARENT_TYPES.has(grandparent.type) &&
      fieldMatches(grandparent, parent, CALLEE_FIELDS)
    ) {
      return "call";
    }
  }
  return "reference";
}

function fieldMatches(parent: Node, child: Node, fields: readonly string[]): boolean {
  for (const field of fields) {
    const f = parent.childForFieldName(field);
    if (f && f.id === child.id) return true;
  }
  return false;
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
