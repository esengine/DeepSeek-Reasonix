import { type Node, Query } from "web-tree-sitter";
import { type GrammarName, getParser, grammarForPath } from "./parser.js";

export type SymbolKind =
  | "function"
  | "class"
  | "interface"
  | "type"
  | "enum"
  | "method"
  | "property"
  | "namespace";

export interface CodeSymbol {
  name: string;
  kind: SymbolKind;
  line: number;
  column: number;
  endLine: number;
  endColumn: number;
  parent?: string;
}

const TS_QUERY = `
(function_declaration name: (identifier) @name) @function
(class_declaration name: (type_identifier) @name) @class
(interface_declaration name: (type_identifier) @name) @interface
(type_alias_declaration name: (type_identifier) @name) @type
(enum_declaration name: (identifier) @name) @enum
(method_definition name: (property_identifier) @name) @method
(public_field_definition name: (property_identifier) @name) @property
(variable_declarator name: (identifier) @name value: [(arrow_function) (function_expression)]) @function
(internal_module name: (identifier) @name) @namespace
`;

const JS_QUERY = `
(function_declaration name: (identifier) @name) @function
(class_declaration name: (identifier) @name) @class
(method_definition name: (property_identifier) @name) @method
(field_definition property: (property_identifier) @name) @property
(variable_declarator name: (identifier) @name value: [(arrow_function) (function_expression)]) @function
`;

const QUERIES: Record<GrammarName, string> = {
  typescript: TS_QUERY,
  tsx: TS_QUERY,
  javascript: JS_QUERY,
};

const KIND_CAPTURE_NAMES = new Set<SymbolKind>([
  "function",
  "class",
  "interface",
  "type",
  "enum",
  "method",
  "property",
  "namespace",
]);

const PARENT_CONTAINER_TYPES = new Set([
  "class_declaration",
  "interface_declaration",
  "internal_module",
]);

export async function extractSymbols(filePath: string, source: string): Promise<CodeSymbol[]> {
  const grammar = grammarForPath(filePath);
  if (!grammar) return [];
  const parser = await getParser(grammar);
  try {
    const tree = parser.parse(source);
    if (!tree) return [];
    const language = parser.language;
    if (!language) return [];
    const query = new Query(language, QUERIES[grammar]);
    try {
      const matches = query.matches(tree.rootNode);
      return matchesToSymbols(matches);
    } finally {
      query.delete();
      tree.delete();
    }
  } finally {
    parser.delete();
  }
}

function matchesToSymbols(
  matches: Array<{ captures: Array<{ name: string; node: Node }> }>,
): CodeSymbol[] {
  const out: CodeSymbol[] = [];
  for (const match of matches) {
    let nameNode: Node | null = null;
    let containerNode: Node | null = null;
    let kind: SymbolKind | null = null;
    for (const cap of match.captures) {
      if (cap.name === "name") {
        nameNode = cap.node;
      } else if (KIND_CAPTURE_NAMES.has(cap.name as SymbolKind)) {
        containerNode = cap.node;
        kind = cap.name as SymbolKind;
      }
    }
    if (!nameNode || !containerNode || !kind) continue;
    out.push({
      name: nameNode.text,
      kind,
      line: containerNode.startPosition.row + 1,
      column: containerNode.startPosition.column + 1,
      endLine: containerNode.endPosition.row + 1,
      endColumn: containerNode.endPosition.column + 1,
      parent: findEnclosingDefinitionName(containerNode),
    });
  }
  out.sort((a, b) => a.line - b.line || a.column - b.column);
  return out;
}

function findEnclosingDefinitionName(node: Node): string | undefined {
  let current = node.parent;
  while (current) {
    if (PARENT_CONTAINER_TYPES.has(current.type)) {
      const nameNode = current.childForFieldName("name");
      if (nameNode) return nameNode.text;
    }
    current = current.parent;
  }
  return undefined;
}
