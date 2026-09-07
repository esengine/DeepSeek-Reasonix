// Where Studio's user inputs are found, once.
//
// A census of what a person can do has one denominator: every production
// registration of a user input. Two readers of that existed here and they
// disagreed — the repository's own gate looked at onClick and onDoubleClick,
// which left 63 onChange, 16 onKeyDown and every DOM listener outside the set
// it was meant to be guarding, about a quarter of the surface. One
// implementation now answers for both.
//
// Certification is two independent questions and neither may answer the other:
// what the registration is on, and whether the event is something a person does
// to the page. An action declaration says which action an input belongs to and
// never whether it is one.

import { parse } from "@babel/parser";

type Node = { type: string; loc?: { start: { line: number } } } & Record<string, unknown>;

// The line these tables draw: does a person's action on the page content
// produce it? Scrolling, wheeling, pointing, dragging and selecting do.
// Resizing the window, focusing it, going offline, a transition ending and a
// media query flipping do not — those are the environment's.
export const USER_INPUT = new Set([
  "click", "dblclick", "auxclick", "contextmenu", "keydown", "keyup", "keypress",
  "pointerdown", "pointerup", "pointermove", "pointercancel", "mousedown", "mouseup",
  "touchstart", "touchend", "touchmove", "input", "change", "submit", "scroll", "selectionchange",
  "paste", "cut", "copy", "drop", "dragstart", "dragover", "dragleave", "dragenter", "dragend", "wheel",
]);
export const RUNTIME_EVENT = new Set([
  "resize", "visibilitychange", "online", "offline", "load", "error", "beforeunload",
  "unload", "message", "storage", "focus", "blur", "transitionend", "animationend", "popstate",
  "hashchange", "languagechange", "themechange", "fullscreenchange", "abort", "close", "open",
]);
/** A DOM node, so an event on it is something a person can aim at. Anything
 *  else that emits events — a media query list, a socket — is not. */
export const DOM_TARGET = /^(HTML\w*Element|SVG\w*Element|Element|EventTarget|Node|Document|Window|ShadowRoot)$/;

/** The event a JSX prop names: onClick is click, onDoubleClick is dblclick. */
export function eventOfProp(name: string): string | null {
  const m = /^on([A-Z]\w*)$/.exec(name ?? "");
  if (!m) return null;
  const e = m[1].toLowerCase();
  return e === "doubleclick" ? "dblclick" : e;
}

export function walk(node: unknown, fn: (n: Node) => void): void {
  if (!node || typeof (node as Node).type !== "string") return;
  const n = node as Node;
  fn(n);
  for (const [k, v] of Object.entries(n)) {
    if (k === "loc" || k.endsWith("Comments")) continue;
    if (Array.isArray(v)) v.forEach((c) => walk(c, fn));
    else walk(v, fn);
  }
}
export function walkStack(node: unknown, fn: (n: Node, stack: Node[]) => void, stack: Node[] = []): void {
  if (!node || typeof (node as Node).type !== "string") return;
  const n = node as Node;
  stack.push(n);
  fn(n, stack);
  for (const [k, v] of Object.entries(n)) {
    if (k === "loc" || k.endsWith("Comments")) continue;
    if (Array.isArray(v)) v.forEach((c) => walkStack(c, fn, stack));
    else walkStack(v, fn, stack);
  }
  stack.pop();
}
export function flat(n: Node | null | undefined): string {
  if (!n) return "?";
  if (n.type === "Identifier") return n.name as string;
  if (n.type === "ThisExpression") return "this";
  if (n.type === "MemberExpression" || n.type === "OptionalMemberExpression") {
    return flat(n.object as Node) + "." + (n.computed ? "[]" : flat(n.property as Node));
  }
  return "?";
}
const jsxName = (n: Node): string =>
  n.type === "JSXIdentifier" ? (n.name as string)
  : n.type === "JSXMemberExpression" ? jsxName(n.object as Node) + "." + jsxName(n.property as Node) : "?";
/** Every id a value can take: one control is genuinely two actions. */
export function allStrings(n: unknown): string[] {
  const out: string[] = [];
  walk(n, (x) => { if (x.type === "StringLiteral") out.push(x.value as string); });
  return out;
}
const firstString = (n: unknown): string | null => allStrings(n)[0] ?? null;
/** What a function is called where it is written: its own id when it has one,
 *  otherwise the binding it is assigned to. */
export function declaredName(fn: Node, parent: Node | undefined): string | null {
  if ((fn.type === "FunctionDeclaration" || fn.type === "FunctionExpression") && (fn.id as Node)?.name) {
    return (fn.id as Node).name as string;
  }
  if (parent?.type === "VariableDeclarator" && (parent.id as Node)?.type === "Identifier") {
    return (parent.id as Node).name as string;
  }
  return null;
}
export function ownerOf(stack: Node[]): string | null {
  for (let i = stack.length - 1; i >= 0; i--) {
    const n = stack[i];
    if (n.type === "ClassMethod" && (n.key as Node)?.name) return (n.key as Node).name as string;
    if (n.type === "ObjectMethod" && (n.key as Node)?.name) return (n.key as Node).name as string;
    if (n.type === "FunctionDeclaration" || n.type === "FunctionExpression" || n.type === "ArrowFunctionExpression") {
      const name = declaredName(n, stack[i - 1]);
      if (name) return name;
      const p = stack[i - 1];
      if (p?.type === "ClassProperty" && (p.key as Node)?.name) return (p.key as Node).name as string;
    }
  }
  return null;
}

export interface Root {
  kind: "jsx-handler" | "dom-event" | "command-chord";
  path: string;
  line: number;
  comp: string | null;
  event: string | null;
  prop?: string;
  actions: string[];
  named: boolean;
  label: string;
  text: string;
  dataTarget: string;
  dataValue: string;
  /** How the listener's target is spelled at the registration, for reports. */
  receiver?: string;
  callback: unknown;
}
export interface Note { path: string; line: number; why: string; detail?: string }
export interface Census {
  roots: Root[];
  nonUser: Note[];
  uncertified: Note[];
  refused: Note[];
}
export interface Options {
  skip?: (path: string) => boolean;
  resolve?: (from: string, spec: string) => string | null;
}

/** Sources, or trees already parsed by the caller. A second reader that parses
 *  its own copy would compare two ASTs and call them one universe. */
/** Whether this file declares the name itself, for a component used where it
 *  is written rather than imported. */
function declaresLocally(tree: Node, name: string): boolean {
  let found = false;
  walk(tree, (d) => {
    if ((d.type === "FunctionDeclaration" || d.type === "ClassDeclaration") && (d.id as Node)?.name === name) found = true;
    if (d.type === "VariableDeclarator" && (d.id as Node)?.type === "Identifier" && (d.id as Node).name === name) found = true;
  });
  return found;
}

/** What both readers of a tree need: the module graph, the bindings a file
 *  declares, and what a receiver expression is proven to be. Built once and
 *  handed to whoever asks, so a second reader cannot answer differently. */
function analysisContext(sources: Map<string, string | Node>, opts: Options) {
  const trees = new Map<string, Node>();
  for (const [path, src] of sources) {
    trees.set(path, typeof src === "string"
      ? (parse(src, { sourceType: "module", plugins: ["typescript", "jsx"], errorRecovery: true }) as unknown as Node)
      : src);
  }
  const importsOf = new Map<string, Map<string, { file: string; name: string }>>();
  for (const [path, tree] of trees) {
    const m = new Map<string, { file: string; name: string }>();
    walk(tree, (n) => {
      const spec = (n.type === "ImportDeclaration" || n.type === "ExportNamedDeclaration") && (n.source as Node)?.value;
      if (typeof spec !== "string") return;
      const target = opts.resolve?.(path, spec) ?? null;
      if (!target) return;
      for (const sp of ((n.specifiers as Node[]) ?? [])) {
        if (sp.type === "ImportSpecifier") {
          m.set((sp.local as Node).name as string, { file: target, name: (sp.imported as Node).name as string });
        }
      }
    });
    importsOf.set(path, m);
  }
  const localsCache = new Map<string, Map<string, Node>>();
  const localsOf = (path: string) => {
    const hit = localsCache.get(path);
    if (hit) return hit;
    const m = new Map<string, Node>();
    walk(trees.get(path), (n) => {
      if (n.type === "VariableDeclarator" && (n.id as Node)?.type === "Identifier" && n.init) {
        m.set((n.id as Node).name as string, n.init as Node);
      }
    });
    localsCache.set(path, m);
    return m;
  };
  const typeArgOf = (ann: Node | undefined | null): string | null => {
    const t = (ann?.type === "TSTypeAnnotation" ? ann.typeAnnotation : ann) as Node | undefined;
    const a = (t?.typeParameters ?? t?.typeArguments) as Node | undefined;
    const first = (a?.params as Node[])?.[0];
    if (!first) return null;
    if (first.type === "TSTypeReference") return flat(first.typeName as Node);
    if (first.type === "TSUnionType") {
      const named = (first.types as Node[]).filter((x) => x.type === "TSTypeReference").map((x) => flat(x.typeName as Node));
      return named.length === 1 ? named[0] : null;
    }
    return null;
  };
  // What a ref-shaped binding holds, so `scroll: RefObject<HTMLDivElement>`
  // says which node a listener on scroll.current is on.
  const refArgs = new Map<string, string>();
  for (const [path, tree] of trees) {
    walk(tree, (n) => {
      if (n.type === "TSPropertySignature" && (n.key as Node)?.name) {
        const a = typeArgOf(n.typeAnnotation as Node);
        if (a) refArgs.set(path + "#" + ((n.key as Node).name as string), a);
      }
      if (n.type === "VariableDeclarator" && (n.id as Node)?.type === "Identifier") {
        const a = typeArgOf((n.id as Node).typeAnnotation as Node)
          ?? (isCall(n.init as Node) ? typeArgOf(n.init as Node) : null);
        if (a) refArgs.set(path + "#" + ((n.id as Node).name as string), a);
      }
    });
  }
  const targetTypeOf = (node: Node | null, path: string, depth = 0): string | null => {
    if (!node || depth > 6) return null;
    if (node.type === "TSAsExpression" || node.type === "TSNonNullExpression") return targetTypeOf(node.expression as Node, path, depth + 1);
    if (node.type === "Identifier") {
      if (/^(window|document|globalThis)$/.test(node.name as string)) return "Window";
      const init = localsOf(path).get(node.name as string);
      return init ? targetTypeOf(init, path, depth + 1) : null;
    }
    if (node.type === "MemberExpression" || node.type === "OptionalMemberExpression") {
      const prop = (node.property as Node)?.name as string | undefined;
      if (/^(window|document|globalThis)$/.test(flat(node))) return "Window";
      if (prop === "currentTarget" || prop === "target") return "EventTarget";
      if (prop !== "current" || (node.object as Node)?.type !== "Identifier") return null;
      return refArgs.get(path + "#" + ((node.object as Node).name as string)) ?? null;
    }
    if (isCall(node) || node.type === "NewExpression") {
      const callee = flat(node.callee as Node).split(".").pop() ?? "";
      if (callee === "matchMedia") return "MediaQueryList";
      if (/^(querySelector|getElementById|closest|createElement)$/.test(callee)) return "Element";
      if (callee === "useRef") return typeArgOf(node);
      if (node.type === "NewExpression" && (node.callee as Node)?.type === "Identifier") return (node.callee as Node).name as string;
    }
    return null;
  };
  // Every name an import binds in a file, resolvable or not. A binding whose
  // module this scan cannot reach still shadows the global of that name, and
  // treating it as absent is how a local wins the platform's identity.
  const importedNames = new Map<string, Set<string>>();
  for (const [path, tree] of trees) {
    const names = new Set<string>();
    walk(tree, (n) => {
      if (n.type !== "ImportDeclaration") return;
      for (const sp of ((n.specifiers as Node[]) ?? [])) names.add((sp.local as Node).name as string);
    });
    importedNames.set(path, names);
  }
  return { trees, importsOf, localsOf, targetTypeOf, importedNames };
}

/** One registration of a listener with the platform, and how much of it is
 *  proven. Produced once; the root census, the effect walk and the reachability
 *  classifier each read the projection they need. A consumer that re-derived it
 *  from the callee's spelling would be a second authority, and the two would
 *  disagree the way the two readers of a JSX handler once did. */
export interface EventRegistration {
  operation: "ADD" | "REMOVE";
  path: string;
  line: number;
  /** The call itself. Node identity is how a consumer walking the same tree
   *  says "this call", rather than matching a name a second time. */
  node: Node;
  /** What the listener is installed on, once proven. Null when it is not. */
  target: string | null;
  targetProvenance: "implicit-global" | "typed-receiver" | "declared-wrapper" | null;
  event: string | null;
  listener: Node | null;
  /** The function the registration is written in, so a consumer never has to
   *  recover a stack the certifier already had. */
  comp: string | null;
  /** Why this call is not a proven platform registration, when it is not. A
   *  refused fact is still a fact: it says the call was considered and what was
   *  missing, so no consumer may quietly treat it as proven. */
  refusal: string | null;
  /** Through the product's own listenAction, which carries the action id. */
  declared: boolean;
  spec: Node | null;
  receiver: string;
}

const REGISTRATION = /^(add|remove)EventListener$/;

/** Whether an enclosing scope binds this name, so a bare call to it is that
 *  binding rather than the global. A parameter named addEventListener is a
 *  parameter; the platform's identity is not something a spelling confers. */
/** What counts as a call, decided once.
 *
 *  Babel gives `a?.()` a different node type from `a()`, so every consumer that
 *  tested for CallExpression by hand dropped optional calls without saying so:
 *  the census read `mutation?.()` as a clean handler and `mutation()` as a
 *  write. One character, and an execution edge deleted rather than reported.
 *
 *  Optionality is carried, never used to excuse. What every consumer asks is
 *  whether an execution can reach something, and a call that may be skipped
 *  still may happen. */
export function isCall(n: Node | null | undefined): boolean {
  return n?.type === "CallExpression" || n?.type === "OptionalCallExpression";
}
export function callMode(n: Node): "REQUIRED" | "OPTIONAL" {
  return n.type === "OptionalCallExpression" ? "OPTIONAL" : "REQUIRED";
}
export function callSite(n: Node | null | undefined) {
  return isCall(n)
    ? { node: n as Node, callee: (n as Node).callee as Node, args: ((n as Node).arguments as Node[]) ?? [], mode: callMode(n as Node) }
    : null;
}

export function bindsName(pat: Node | null | undefined, name: string): boolean {
  if (!pat) return false;
  if (pat.type === "Identifier") return pat.name === name;
  if (pat.type === "AssignmentPattern") return bindsName(pat.left as Node, name);
  if (pat.type === "RestElement") return bindsName(pat.argument as Node, name);
  if (pat.type === "ArrayPattern") return ((pat.elements as Node[]) ?? []).some((x) => bindsName(x, name));
  if (pat.type === "ObjectPattern") {
    return ((pat.properties as Node[]) ?? []).some((x) => bindsName((x.value ?? x.argument) as Node, name));
  }
  return false;
}

/** Whether anything in `stack` binds `name`, so a use of it inside is that
 *  binding rather than whatever owns the name further out. */
export function bindingInScope(name: string, stack: Node[]): boolean {
  const binds = (pat: Node | null | undefined) => bindsName(pat, name);
  for (const n of stack) {
    if (/^(FunctionDeclaration|FunctionExpression|ArrowFunctionExpression|ObjectMethod|ClassMethod)$/.test(n.type)) {
      if (((n.params as Node[]) ?? []).some(binds)) return true;
      if ((n.id as Node)?.name === name) return true;
    }
    if (n.type === "CatchClause" && binds(n.param as Node)) return true;
    const body = (n.type === "BlockStatement" || n.type === "Program") ? (n.body as Node[]) : null;
    for (const st of body ?? []) {
      if (st.type === "VariableDeclaration" && ((st.declarations as Node[]) ?? []).some((d) => binds(d.id as Node))) return true;
      if (/^(FunctionDeclaration|ClassDeclaration)$/.test(st.type) && (st.id as Node)?.name === name) return true;
    }
  }
  return false;
}

function shadowed(name: string, stack: Node[], path: string, imported: Map<string, Set<string>>): boolean {
  return bindingInScope(name, stack) || (imported.get(path)?.has(name) ?? false);
}

/** Every event registration in a tree, certified once. */
export function eventRegistrations(sources: Map<string, string | Node>, opts: Options = {}): EventRegistration[] {
  return registrationsIn(analysisContext(sources, opts), opts);
}

function registrationsIn(ctx: ReturnType<typeof analysisContext>, opts: Options): EventRegistration[] {
  const { trees, importsOf, targetTypeOf, importedNames } = ctx;
  const out: EventRegistration[] = [];
  for (const [path, tree] of trees) {
    walkStack(tree, (n, stack) => {
      if (!isCall(n)) return;
      const callee = n.callee as Node;
      const local = flat(callee);
      const imp = local.includes(".") ? undefined : importsOf.get(path)?.get(local);
      const declared = !!imp && imp.name === "listenAction" && trees.has(imp.file);
      const member = callee.type.endsWith("MemberExpression");
      const verb = local.includes(".") ? local.slice(local.lastIndexOf(".") + 1) : local;
      if (!declared && !REGISTRATION.test(verb)) return;
      const operation: "ADD" | "REMOVE" = declared || verb.startsWith("add") ? "ADD" : "REMOVE";
      const args = n.arguments as Node[];
      const line = n.loc!.start.line;
      const spec = declared ? args[2] ?? null : null;
      const recvNode = declared ? args[0] : member ? (callee.object as Node) : null;
      const typeArg = declared ? args[1] : args[0];
      const type = typeArg?.type === "StringLiteral" ? (typeArg.value as string) : null;
      const fact: EventRegistration = {
        operation, path, line, node: n, target: null, targetProvenance: null, event: type,
        comp: ownerOf(stack),
        listener: declared
          ? ((spec?.type === "ObjectExpression"
              ? (spec.properties as Node[]).find((x) => x.type === "ObjectProperty" && ((x.key as Node)?.name as string) === "listener")
              : null)?.value as Node ?? null)
          : args[1] ?? null,
        refusal: null, declared, spec,
        receiver: recvNode ? flat(recvNode) : "<global>",
      };
      // Target and event both arriving as this function's own parameters make
      // it a wrapper, not an entry point: its call sites are the registrations.
      const fn = stack.slice().reverse().find((x) => /Function|ArrowFunctionExpression/.test(x.type));
      const params = new Set(((fn?.params as Node[]) ?? []).filter((x) => x.type === "Identifier").map((x) => x.name as string));
      if (recvNode?.type === "Identifier" && params.has(recvNode.name as string) &&
          typeArg?.type === "Identifier" && params.has(typeArg.name as string)) {
        out.push({ ...fact, refusal: "REGISTRATION_WRAPPER" });
        return;
      }
      // A bare call is the platform's only when nothing nearer owns the name.
      if (!declared && !member && shadowed(local, stack, path, importedNames)) {
        out.push({ ...fact, refusal: "SHADOWED_CALLEE" });
        return;
      }
      if (declared) {
        fact.target = recvNode ? targetTypeOf(recvNode, path) : null;
        fact.targetProvenance = fact.target ? "declared-wrapper" : null;
      } else if (member) {
        fact.target = targetTypeOf(recvNode, path);
        fact.targetProvenance = fact.target ? "typed-receiver" : null;
      } else {
        fact.target = "Window";
        fact.targetProvenance = "implicit-global";
      }
      if (!fact.target) fact.refusal = "UNRESOLVED_RECEIVER";
      out.push(fact);
    });
  }
  void opts;
  return out;
}

export function certifyRoots(sources: Map<string, string | Node>, opts: Options = {}): Census {
  const ctx = analysisContext(sources, opts);
  const { trees, importsOf } = ctx;
  const registrations = registrationsIn(ctx, opts);
  const roots: Root[] = [];
  const nonUser: Note[] = [];
  const uncertified: Note[] = [];
  const refused: Note[] = [];



  for (const [path, tree] of trees) {
    const skipped = opts.skip?.(path) ?? false;
    // 1. JSX handlers. One root per handler, so membership is per handler too:
    // a generic data-action on an element carrying several of them cannot say
    // which input it names, and naming all of them called typing in a box the
    // same action as pressing Enter in it.
    if (!skipped) {
      walkStack(tree, (n, stack) => {
        if (n.type !== "JSXOpeningElement") return;
        const host = ownerOf(stack);
        // An event-like prop on a custom component is wiring, not a
        // registration: `<Composer onSubmit={…}>` hands a callback down, and
        // the input a person performs is the textarea inside Composer, which is
        // already a root of its own. Counting both put the same entry point in
        // the denominator twice.
        //
        // A component whose body this scan excludes is the exception: its own
        // registration is invisible here, so the call site is where the input
        // becomes visible. That is what an interaction primitive is, and it is
        // the same declaration that hides its internals — one fact, not two.
        //
        // A component this scan cannot place at all is neither: nothing proves
        // where its interaction lives, and a prop spelled onClick is not proof.
        const el = jsxName(n.name as Node);
        let abstraction: "host" | "primitive" | "wiring" | "unknown" = "host";
        if (/^[A-Z]/.test(el)) {
          const imp = importsOf.get(path)?.get(el);
          const decl = imp?.file ?? (declaresLocally(trees.get(path)!, imp?.name ?? el) ? path : null);
          abstraction = decl === null ? "unknown" : (opts.skip?.(decl) ?? false) ? "primitive" : "wiring";
        }
        const a: Record<string, unknown> = {};
        for (const at of (n.attributes as Node[])) {
          if (at.type === "JSXAttribute") a[(at.name as Node).name as string] = at.value;
        }
        const label = (a["aria-label"] && firstString(a["aria-label"])) || (a.title && firstString(a.title)) || "";
        const parent = stack[stack.length - 2];
        const text = parent?.type === "JSXElement"
          ? ((parent.children as Node[]) ?? []).map((c) =>
              c.type === "JSXText" ? (c.value as string).trim()
              : c.type === "JSXExpressionContainer" ? firstString(c.expression) ?? "" : "")
              .filter(Boolean).join(" ").replace(/\s+/g, " ").slice(0, 60)
          : "";
        const handlers = Object.keys(a)
          .map((name) => ({ name, event: eventOfProp(name) }))
          .filter((h): h is { name: string; event: string } => !!h.event && USER_INPUT.has(h.event));
        if (handlers.length && abstraction !== "host" && abstraction !== "primitive") {
          const note = { path, line: n.loc!.start.line, why: abstraction === "wiring" ? "COMPONENT_PROP_WIRING" : "UNRESOLVED_INTERACTION_ABSTRACTION", detail: "<" + el + " " + handlers.map((h) => h.name).join(" ") + ">" };
          (abstraction === "wiring" ? nonUser : uncertified).push(note);
          return;
        }
        const generic = "data-action" in a ? allStrings(a["data-action"]) : null;
        if (generic && handlers.length > 1) {
          refused.push({ path, line: n.loc!.start.line, why: "AMBIGUOUS_ELEMENT_ACTION", detail: handlers.map((h) => h.name).join("+") });
        }
        for (const h of handlers) {
          const scoped = a["data-action-" + h.event] ? allStrings(a["data-action-" + h.event]) : null;
          const actions = scoped ?? (handlers.length === 1 ? generic ?? [] : []);
          const value = a[h.name] as Node | undefined;
          roots.push({
            kind: "jsx-handler", path, line: n.loc!.start.line, comp: host, event: h.event, prop: h.name,
            callback: value?.type === "JSXExpressionContainer" ? value.expression : value,
            actions, named: actions.length > 0, label: String(label ?? ""), text,
            dataTarget: a["data-target"] ? firstString(a["data-target"]) ?? "<expr>" : "",
            dataValue: a["data-value"] ? firstString(a["data-value"]) ?? "<expr>" : "",
          });
        }
      });
    }

    // 2. The window's shortcut table: a chord and the action it performs.
    walkStack(tree, (n, stack) => {
      if (n.type !== "ObjectExpression") return;
      const props = (n.properties as Node[]).filter((x) => x.type === "ObjectProperty");
      const at = (want: string) => props.find((x) => ((x.key as Node)?.name as string) === want);
      if (!at("chord") || !at("run")) return;
      const actions = allStrings(at("action")?.value);
      roots.push({
        kind: "command-chord", path, line: n.loc!.start.line, comp: ownerOf(stack), event: null,
        callback: at("run")!.value, actions, named: actions.length > 0,
        label: firstString(at("chord")!.value) ?? "", text: "", dataTarget: "", dataValue: "",
      });
    });

    // 3. DOM listeners. The registration fact is already certified; what this
    // adds is membership authority, which is a projection of it: an ADD whose
    // target is a DOM node and whose event is something a person does. Written
    // on a resize or a media query the declaration is an error rather than a
    // promotion.
    for (const reg of registrations) {
      if (reg.path !== path) continue;
      const { line } = reg;
      const specProps = reg.spec?.type === "ObjectExpression"
        ? (reg.spec.properties as Node[]).filter((x) => x.type === "ObjectProperty") : null;
      const specAt = (want: string) => specProps?.find((x) => ((x.key as Node)?.name as string) === want);
      const record = (why: string) => {
        if (reg.declared) refused.push({ path, line, why: "ACTION_ON_A_NON_INPUT", detail: why });
        (why === "UNRESOLVED_RECEIVER" || why === "UNRESOLVED_EVENT_TYPE" ? uncertified : nonUser).push({ path, line, why });
      };
      // Membership is a property of installing a listener. Removing one takes
      // an entry point away and never creates one, so it is read here and
      // nowhere projected into the denominator.
      if (reg.operation !== "ADD") continue;
      // A name a nearer scope owns is that binding's call, not the platform's.
      // It registers nothing here, so it is neither an entry point nor a hole:
      // whatever the binding is, it is reached as an ordinary call.
      if (reg.refusal === "SHADOWED_CALLEE") continue;
      if (reg.refusal === "REGISTRATION_WRAPPER") { nonUser.push({ path, line, why: "REGISTRATION_WRAPPER" }); continue; }
      if (reg.declared && !specProps) { record("UNRESOLVED_EVENT_TYPE"); continue; }
      if (!reg.target) { record("UNRESOLVED_RECEIVER"); continue; }
      if (!reg.event) { record("UNRESOLVED_EVENT_TYPE"); continue; }
      if (!DOM_TARGET.test(reg.target)) { record("NOT_A_DOM_TARGET:" + reg.target); continue; }
      if (RUNTIME_EVENT.has(reg.event)) { record("RUNTIME_EVENT"); continue; }
      if (!USER_INPUT.has(reg.event)) { record("UNRESOLVED_EVENT_TYPE"); continue; }
      const actions = reg.declared ? allStrings(specAt("action")?.value) : [];
      roots.push({
        kind: "dom-event", path, line, comp: reg.comp, event: reg.event,
        callback: reg.declared ? specAt("listener")?.value : reg.listener,
        actions, named: actions.length > 0, label: reg.event, text: "",
        receiver: reg.receiver,
        dataTarget: "", dataValue: reg.declared ? firstString(specAt("value")?.value) ?? "" : "",
      });
    }
  }
  return { roots, nonUser, uncertified, refused };
}
