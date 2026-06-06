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

const PYTHON_QUERY = `
(function_definition name: (identifier) @name) @function
(class_definition name: (identifier) @name) @class
`;

const GO_QUERY = `
(function_declaration name: (identifier) @name) @function
(method_declaration name: (field_identifier) @name) @method
(type_spec name: (type_identifier) @name type: (struct_type)) @class
(type_spec name: (type_identifier) @name type: (interface_type)) @interface
(type_spec name: (type_identifier) @name) @type
`;

const RUST_QUERY = `
(function_item name: (identifier) @name) @function
(struct_item name: (type_identifier) @name) @class
(enum_item name: (type_identifier) @name) @enum
(trait_item name: (type_identifier) @name) @interface
(type_item name: (type_identifier) @name) @type
(mod_item name: (identifier) @name) @namespace
(const_item name: (identifier) @name) @property
(static_item name: (identifier) @name) @property
`;

const JAVA_QUERY = `
(class_declaration name: (identifier) @name) @class
(interface_declaration name: (identifier) @name) @interface
(enum_declaration name: (identifier) @name) @enum
(method_declaration name: (identifier) @name) @method
(constructor_declaration name: (identifier) @name) @method
(field_declaration declarator: (variable_declarator name: (identifier) @name)) @property
`;

const QUERIES: Record<GrammarName, string> = {
  typescript: TS_QUERY,
  tsx: TS_QUERY,
  javascript: JS_QUERY,
  python: PYTHON_QUERY,
  go: GO_QUERY,
  rust: RUST_QUERY,
  java: JAVA_QUERY,
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
  "class_definition",
  "impl_item",
  "trait_item",
  "mod_item",
]);

const METHOD_PROMOTING_CONTAINER_TYPES = new Set([
  "class_declaration",
  "class_definition",
  "interface_declaration",
  "impl_item",
  "trait_item",
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
    const enclosing = findEnclosingContainer(containerNode);
    if (kind === "function" && enclosing && METHOD_PROMOTING_CONTAINER_TYPES.has(enclosing.type)) {
      kind = "method";
    }
    out.push({
      name: nameNode.text,
      kind,
      line: containerNode.startPosition.row + 1,
      column: containerNode.startPosition.column + 1,
      endLine: containerNode.endPosition.row + 1,
      endColumn: containerNode.endPosition.column + 1,
      parent: enclosing ? containerNameOf(enclosing) : undefined,
    });
  }
  out.sort((a, b) => a.line - b.line || a.column - b.column);
  return out;
}

function findEnclosingContainer(node: Node): Node | null {
  let current = node.parent;
  while (current) {
    if (PARENT_CONTAINER_TYPES.has(current.type)) return current;
    current = current.parent;
  }
  return null;
}

function containerNameOf(container: Node): string | undefined {
  if (container.type === "impl_item") {
    const typeField = container.childForFieldName("type");
    if (typeField) return typeField.text;
  }
  const nameField = container.childForFieldName("name");
  return nameField?.text;
}
