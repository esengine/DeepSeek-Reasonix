import { parse } from "acorn";
import type { Node } from "acorn";
import type { WorkflowMeta, WorkflowMetaPhase } from "./types.js";

export interface ParsedWorkflowScript {
  meta: WorkflowMeta;
  body: string;
  diagnostics: string[];
}

type AnyNode = Node & Record<string, unknown> & { start: number; end: number };

export function parseWorkflowScript(script: string): ParsedWorkflowScript {
  const ast = parse(script, {
    ecmaVersion: "latest",
    sourceType: "module",
    allowAwaitOutsideFunction: true,
    allowReturnOutsideFunction: true,
  }) as unknown as AnyNode;

  rejectUnsafeNodes(ast);

  const body = nodeArray(ast.body);
  const first = body[0];
  if (!first || first.type !== "ExportNamedDeclaration") {
    throw new Error(
      "`export const meta = { name, description, phases }` must be the first statement in the script",
    );
  }

  const declaration = nodeValue(first.declaration);
  if (!declaration || declaration.type !== "VariableDeclaration" || declaration.kind !== "const") {
    throw new Error("meta export must be `export const meta = ...`");
  }

  const declarations = nodeArray(declaration.declarations);
  if (declarations.length !== 1) {
    throw new Error("meta export must declare only `meta`");
  }

  const declarator = declarations[0]!;
  const id = nodeValue(declarator.id);
  if (!id || id.type !== "Identifier" || id.name !== "meta") {
    throw new Error("meta export must declare `meta`");
  }

  const init = nodeValue(declarator.init);
  if (!init) throw new Error("meta must have a literal value");
  const meta = evaluateLiteral(init, "meta");
  validateMeta(meta);

  return {
    meta,
    body: script.slice(0, first.start) + script.slice(first.end),
    diagnostics: [],
  };
}

function rejectUnsafeNodes(node: AnyNode): void {
  if (node.type === "ImportDeclaration" || node.type === "ImportExpression") {
    throw new Error(`unsafe workflow script API: ${node.type} is not allowed`);
  }

  if (node.type === "CallExpression") {
    const callee = nodeValue(node.callee);
    if (callee?.type === "Identifier" && callee.name === "require") {
      throw new Error("unsafe workflow script API: require() is not allowed");
    }
  }

  if (node.type === "NewExpression") {
    const callee = nodeValue(node.callee);
    if (callee?.type === "Identifier" && callee.name === "Date") {
      throw new Error("workflow scripts must be deterministic: new Date() is not allowed");
    }
  }

  if (node.type === "MemberExpression") {
    const path = memberPath(node);
    if (path === "Date.now") {
      throw new Error("workflow scripts must be deterministic: Date.now() is not allowed");
    }
    if (path === "Math.random") {
      throw new Error("workflow scripts must be deterministic: Math.random() is not allowed");
    }
    if (path === "process.env" || path.startsWith("process.env.")) {
      throw new Error("unsafe workflow script API: process.env is not allowed");
    }
  }

  for (const value of Object.values(node)) {
    if (Array.isArray(value)) {
      for (const item of value) {
        if (isNode(item)) rejectUnsafeNodes(item);
      }
    } else if (isNode(value)) {
      rejectUnsafeNodes(value);
    }
  }
}

function evaluateLiteral(node: AnyNode, path: string): unknown {
  switch (node.type) {
    case "ObjectExpression": {
      const out: Record<string, unknown> = {};
      for (const prop of nodeArray(node.properties)) {
        if (prop.type === "SpreadElement") throw new Error(`spread not allowed in ${path}`);
        if (prop.type !== "Property") throw new Error(`only plain properties allowed in ${path}`);
        if (prop.computed === true) throw new Error(`computed keys not allowed in ${path}`);
        if (prop.kind !== "init" || prop.method === true) {
          throw new Error(`methods/accessors not allowed in ${path}`);
        }
        const key = propertyKey(nodeValue(prop.key), path);
        if (key === "__proto__" || key === "constructor" || key === "prototype") {
          throw new Error(`reserved key name not allowed in ${path}: ${key}`);
        }
        const value = nodeValue(prop.value);
        if (!value) throw new Error(`missing value in ${path}.${key}`);
        out[key] = evaluateLiteral(value, `${path}.${key}`);
      }
      return out;
    }
    case "ArrayExpression":
      return arrayElements(node, path).map((element, index) => {
        if (element.type === "SpreadElement") throw new Error(`spread not allowed in ${path}`);
        return evaluateLiteral(element, `${path}[${index}]`);
      });
    case "Literal":
      return node.value;
    case "TemplateLiteral": {
      const expressions = nodeArray(node.expressions);
      if (expressions.length > 0) throw new Error(`template interpolation not allowed in ${path}`);
      return nodeArray(node.quasis)
        .map((quasi) => templateText(quasi))
        .join("");
    }
    case "UnaryExpression": {
      const argument = nodeValue(node.argument);
      if (
        node.operator === "-" &&
        argument?.type === "Literal" &&
        typeof argument.value === "number"
      ) {
        return -argument.value;
      }
      throw new Error(`only negative-number unary allowed in ${path}`);
    }
    default:
      throw new Error(`non-literal node type in ${path}: ${node.type}`);
  }
}

function validateMeta(meta: unknown): asserts meta is WorkflowMeta {
  if (!meta || typeof meta !== "object") throw new Error("meta must be an object");
  const value = meta as Record<string, unknown>;
  if (typeof value.name !== "string" || !value.name.trim()) {
    throw new Error("meta.name must be a non-empty string");
  }
  if (typeof value.description !== "string" || !value.description.trim()) {
    throw new Error("meta.description must be a non-empty string");
  }
  if (value.whenToUse !== undefined && typeof value.whenToUse !== "string") {
    throw new Error("meta.whenToUse must be a string");
  }
  if (value.phases !== undefined) {
    if (!Array.isArray(value.phases)) throw new Error("meta.phases must be an array");
    for (const phase of value.phases) {
      validateMetaPhase(phase);
    }
  }
}

function validateMetaPhase(phase: unknown): asserts phase is WorkflowMetaPhase {
  if (!phase || typeof phase !== "object") {
    throw new Error("each meta phase must have a title string");
  }
  const value = phase as Record<string, unknown>;
  if (typeof value.title !== "string" || !value.title.trim()) {
    throw new Error("each meta phase must have a title string");
  }
  if (value.detail !== undefined && typeof value.detail !== "string") {
    throw new Error("meta phase detail must be a string");
  }
  if (value.model !== undefined && typeof value.model !== "string") {
    throw new Error("meta phase model must be a string");
  }
}

function propertyKey(node: AnyNode | null, path: string): string {
  if (!node) throw new Error(`missing key in ${path}`);
  if (node.type === "Identifier" && typeof node.name === "string") return node.name;
  if (
    node.type === "Literal" &&
    (typeof node.value === "string" || typeof node.value === "number")
  ) {
    return String(node.value);
  }
  throw new Error(`unsupported key type in ${path}: ${node.type}`);
}

function memberPath(node: AnyNode): string {
  const object = nodeValue(node.object);
  const property = nodeValue(node.property);
  const objectPath = object
    ? object.type === "MemberExpression"
      ? memberPath(object)
      : nameOf(object)
    : "";
  const propertyName = property ? nameOf(property) : "";
  return [objectPath, propertyName].filter(Boolean).join(".");
}

function nameOf(node: AnyNode): string {
  if (node.type === "Identifier" && typeof node.name === "string") return node.name;
  if (node.type === "Literal" && typeof node.value === "string") return node.value;
  return "";
}

function arrayElements(node: AnyNode, path: string): AnyNode[] {
  const raw = node.elements;
  if (!Array.isArray(raw)) return [];
  return raw.map((element, index) => {
    if (!isNode(element)) throw new Error(`sparse arrays not allowed in ${path}`);
    return element;
  });
}

function templateText(node: AnyNode): string {
  const value = node.value;
  if (!value || typeof value !== "object") return "";
  const cooked = (value as Record<string, unknown>).cooked;
  const raw = (value as Record<string, unknown>).raw;
  return typeof cooked === "string" ? cooked : typeof raw === "string" ? raw : "";
}

function nodeArray(value: unknown): AnyNode[] {
  return Array.isArray(value) ? value.filter(isNode) : [];
}

function nodeValue(value: unknown): AnyNode | null {
  return isNode(value) ? value : null;
}

function isNode(value: unknown): value is AnyNode {
  return Boolean(
    value && typeof value === "object" && typeof (value as { type?: unknown }).type === "string",
  );
}
