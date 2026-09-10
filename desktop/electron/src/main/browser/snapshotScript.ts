/// <reference lib="dom" />

// Page-side snapshot walker. It is serialised with Function.prototype.toString
// and executed inside the website, so it must stay self-contained: no imports,
// no references to module scope, plain ES2022 only.

export interface SnapshotInput {
  key: string;
  snapshotId: string;
  prefix: string;
  selector: string;
  budget: number;
}

export interface SnapshotOutput {
  docId: string;
  tree: string;
  refs: number;
  nodes: number;
  truncated: number;
}

export interface PageRegistry {
  docId: string;
  snapshotId: string;
  refs: Map<string, Element>;
}

export function pageSnapshot(input: SnapshotInput): SnapshotOutput {
  const host = window as unknown as Record<string, unknown>;
  let registry = host[input.key] as PageRegistry | undefined;
  if (!registry || typeof registry.docId !== "string") {
    const random = Math.random().toString(36).slice(2) + Math.random().toString(36).slice(2);
    registry = { docId: `${performance.timeOrigin}:${random}`, snapshotId: "", refs: new Map() };
    Object.defineProperty(host, input.key, { value: registry, enumerable: false, configurable: true, writable: true });
  }
  registry.snapshotId = input.snapshotId;
  registry.refs = new Map();
  const refs = registry.refs;
  const docId = registry.docId;

  let root: Element | null = document.body ?? document.documentElement;
  if (input.selector !== "") {
    try {
      root = document.querySelector(input.selector);
    } catch {
      root = null;
    }
    if (!root) return { docId, tree: `(no element matches selector ${JSON.stringify(input.selector)})`, refs: 0, nodes: 0, truncated: 0 };
  }

  const SKIP = new Set(["SCRIPT", "STYLE", "NOSCRIPT", "TEMPLATE", "HEAD", "META", "LINK", "TITLE", "SVG", "CANVAS", "VIDEO", "AUDIO", "SOURCE", "TRACK", "MAP", "AREA", "PATH", "DATALIST"]);
  const TAG_ROLES: Record<string, string> = {
    BUTTON: "button", TEXTAREA: "textbox", OPTION: "option", OPTGROUP: "group", TABLE: "table", TR: "row", TD: "cell", TH: "columnheader",
    UL: "list", OL: "list", LI: "listitem", NAV: "navigation", MAIN: "main", FORM: "form", DIALOG: "dialog", IMG: "img", HEADER: "banner",
    FOOTER: "contentinfo", ASIDE: "complementary", ARTICLE: "article", SECTION: "region", SUMMARY: "button", DETAILS: "group", IFRAME: "iframe",
    FRAME: "iframe", MENU: "list", FIELDSET: "group", PROGRESS: "progressbar", METER: "meter", HR: "separator", H1: "heading", H2: "heading",
    H3: "heading", H4: "heading", H5: "heading", H6: "heading",
  }
  const INTERACTIVE = new Set(["button", "link", "textbox", "searchbox", "checkbox", "radio", "combobox", "listbox", "option", "menuitem", "menuitemcheckbox", "menuitemradio", "tab", "slider", "switch", "spinbutton", "treeitem", "iframe"]);
  const CONTENT_NAMED = new Set(["button", "link", "heading", "option", "menuitem", "menuitemcheckbox", "menuitemradio", "tab", "cell", "columnheader", "rowheader", "treeitem", "listitem", "summary"]);
  const TEXT_INPUTS = new Set(["text", "email", "tel", "url", "number", "date", "datetime-local", "month", "week", "time", "color", ""]);

  const lines: string[] = [];
  let nodes = 0;
  let truncated = 0;
  let refCount = 0;
  const active = document.activeElement;

  function clip(value: string): string {
    const text = value.replace(/\s+/g, " ").trim();
    return text.length > 120 ? `${text.slice(0, 119)}…` : text;
  }

  function roleOf(el: Element): string {
    const explicit = (el.getAttribute("role") ?? "").trim().toLowerCase();
    if (explicit !== "") return explicit.split(/\s+/)[0];
    const tag = el.tagName;
    if (tag === "A") return el.hasAttribute("href") ? "link" : "generic";
    if (tag === "INPUT") {
      const type = ((el as HTMLInputElement).type || "text").toLowerCase();
      if (type === "button" || type === "submit" || type === "reset" || type === "image" || type === "file") return "button";
      if (type === "checkbox" || type === "radio") return type;
      if (type === "range") return "slider";
      if (type === "search") return "searchbox";
      if (type === "hidden") return "hidden";
      if (type === "password") return "textbox";
      return TEXT_INPUTS.has(type) ? "textbox" : "textbox";
    }
    if (tag === "SELECT") return (el as HTMLSelectElement).multiple ? "listbox" : "combobox";
    if (tag === "SECTION" && !el.hasAttribute("aria-label") && !el.hasAttribute("aria-labelledby")) return "generic";
    const mapped = TAG_ROLES[tag];
    if (mapped) return mapped;
    const tabindex = el.getAttribute("tabindex");
    if (tabindex !== null && Number(tabindex) >= 0) return "generic-clickable";
    return "generic";
  }

  function labelledBy(el: Element): string {
    const ids = (el.getAttribute("aria-labelledby") ?? "").split(/\s+/).filter((id) => id !== "");
    return ids.map((id) => document.getElementById(id)?.textContent ?? "").join(" ");
  }

  function nameOf(el: Element, role: string): string {
    const aria = el.getAttribute("aria-label");
    if (aria && aria.trim() !== "") return clip(aria);
    const byId = labelledBy(el);
    if (byId.trim() !== "") return clip(byId);
    const tag = el.tagName;
    if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || tag === "METER" || tag === "PROGRESS") {
      const labels = (el as HTMLInputElement).labels;
      if (labels && labels.length) return clip([...labels].map((label) => label.textContent ?? "").join(" "));
    }
    if (tag === "INPUT") {
      const inputEl = el as HTMLInputElement;
      const type = inputEl.type.toLowerCase();
      if ((type === "button" || type === "submit" || type === "reset") && inputEl.value !== "") return clip(inputEl.value);
      if (type === "image" && inputEl.alt !== "") return clip(inputEl.alt);
    }
    if (tag === "IMG") return clip((el as HTMLImageElement).alt);
    if (tag === "IFRAME" || tag === "FRAME") return clip(el.getAttribute("title") ?? el.getAttribute("name") ?? "");
    const title = el.getAttribute("title");
    if (title && title.trim() !== "") return clip(title);
    const placeholder = el.getAttribute("placeholder");
    if (placeholder && placeholder.trim() !== "") return clip(placeholder);
    if (CONTENT_NAMED.has(role) || role === "generic-clickable") return clip(el.textContent ?? "");
    return "";
  }

  function statesOf(el: Element, role: string): string[] {
    const states: string[] = [];
    const tag = el.tagName;
    const inputEl = el as HTMLInputElement;
    if (tag === "INPUT" && (inputEl.type === "checkbox" || inputEl.type === "radio")) {
      if (inputEl.indeterminate) states.push("mixed");
      else if (inputEl.checked) states.push("checked");
    } else if (el.getAttribute("aria-checked") === "true") states.push("checked");
    else if (el.getAttribute("aria-checked") === "mixed") states.push("mixed");
    if (el.getAttribute("aria-pressed") === "true") states.push("pressed");
    if ((el as HTMLButtonElement).disabled === true || el.getAttribute("aria-disabled") === "true") states.push("disabled");
    const expanded = el.getAttribute("aria-expanded");
    if (expanded === "true" || (tag === "DETAILS" && (el as HTMLDetailsElement).open)) states.push("expanded");
    else if (expanded === "false") states.push("collapsed");
    if ((tag === "OPTION" && (el as HTMLOptionElement).selected) || el.getAttribute("aria-selected") === "true") states.push("selected");
    if (el === active) states.push("focused");
    if (role === "heading") {
      const level = el.getAttribute("aria-level") ?? (/^H([1-6])$/.exec(tag)?.[1] ?? "2");
      states.push(`level=${level}`);
    }
    if (tag === "INPUT" && inputEl.type === "password") states.push("password");
    else if (tag === "INPUT" && inputEl.type === "file") states.push("file");
    else if (tag === "INPUT" && inputEl.type !== "checkbox" && inputEl.type !== "radio" && inputEl.type !== "submit" && inputEl.type !== "button" && inputEl.type !== "reset" && inputEl.type !== "image") {
      if (inputEl.value !== "") states.push(`value=${JSON.stringify(clip(inputEl.value))}`);
    } else if (tag === "TEXTAREA") {
      const value = (el as HTMLTextAreaElement).value;
      if (value !== "") states.push(`value=${JSON.stringify(clip(value))}`);
    } else if (tag === "SELECT") {
      const select = el as HTMLSelectElement;
      const chosen = [...select.selectedOptions].map((option) => option.label || option.text);
      if (chosen.length) states.push(`value=${JSON.stringify(clip(chosen.join(", ")))}`);
      if (select.multiple) states.push("multiple");
    }
    if (role === "generic-clickable") states.push("clickable");
    if (tag === "A" && el.hasAttribute("href")) {
      const href = el.getAttribute("href") ?? "";
      if (href.startsWith("#")) states.push(`href=${JSON.stringify(clip(href))}`);
    }
    return states;
  }

  function hiddenSubtree(el: Element): boolean {
    // Uploads target hidden file inputs, so they must keep their snapshot ref.
    if (el.tagName === "INPUT" && (el as HTMLInputElement).type === "file") return false;
    if (el.getAttribute("aria-hidden") === "true") return true;
    if (el.tagName === "INPUT" && (el as HTMLInputElement).type === "hidden") return true;
    const style = window.getComputedStyle(el);
    return style.display === "none" || style.visibility === "hidden";
  }

  function zeroSize(el: Element): boolean {
    if (el.tagName === "OPTION" || el.tagName === "OPTGROUP") return false;
    if (el.tagName === "INPUT" && (el as HTMLInputElement).type === "file") return false;
    const rect = el.getBoundingClientRect();
    return rect.width === 0 && rect.height === 0 && el !== active;
  }

  function emit(line: string): void {
    if (nodes >= input.budget) {
      truncated += 1;
      return;
    }
    nodes += 1;
    lines.push(line);
  }

  function visit(node: Node, depth: number, suppressText: boolean): void {
    if (node.nodeType === Node.TEXT_NODE) {
      if (suppressText) return;
      const text = clip(node.textContent ?? "");
      if (text !== "") emit(`${"  ".repeat(depth)}text ${JSON.stringify(text)}`);
      return;
    }
    if (node.nodeType !== Node.ELEMENT_NODE) return;
    const el = node as Element;
    if (SKIP.has(el.tagName) || hiddenSubtree(el)) return;
    const role = roleOf(el);
    if (role === "hidden") return;
    // A <label> surfaces as its control's name, and a zero-size box is
    // invisible: neither contributes a subtree.
    if (el.tagName === "LABEL" && (el as HTMLLabelElement).control !== null) return;
    if (zeroSize(el)) return;
    let childDepth = depth;
    let childSuppress = suppressText;
    if (role !== "generic") {
      const name = nameOf(el, role);
      const states = statesOf(el, role);
      const displayRole = role === "generic-clickable" ? "generic" : role;
      const interactive = INTERACTIVE.has(role) || role === "generic-clickable";
      let line = `${"  ".repeat(depth)}${displayRole}`;
      if (name !== "") line += ` ${JSON.stringify(name)}`;
      if (states.length) line += ` [${states.join(", ")}]`;
      if ((interactive || name !== "") && role !== "iframe") {
        refCount += 1;
        const ref = `${input.prefix}e${refCount}`;
        refs.set(ref, el);
        line += ` ref=${ref}`;
      }
      emit(line);
      childDepth = depth + 1;
      if (CONTENT_NAMED.has(role) || role === "generic-clickable") childSuppress = true;
    }
    // A textarea's text child duplicates the value state.
    if (el.tagName === "TEXTAREA") return;
    if (el.tagName === "SELECT" && role !== "generic") {
      for (const option of (el as HTMLSelectElement).options) visit(option, childDepth, false);
      return;
    }
    const shadow = (el as HTMLElement).shadowRoot;
    const children = shadow ? [...shadow.childNodes, ...el.childNodes] : [...el.childNodes];
    for (const child of children) visit(child, childDepth, childSuppress);
  }

  // A selector scopes INTO the element: its own line is omitted and the tree
  // starts at its children (including a shadow root's), all at depth zero.
  if (input.selector !== "") {
    const scopedShadow = (root as HTMLElement).shadowRoot;
    const scopedChildren = scopedShadow ? [...scopedShadow.childNodes, ...root.childNodes] : [...root.childNodes];
    for (const child of scopedChildren) visit(child, 0, false);
  } else {
    visit(root, 0, false);
  }
  if (truncated > 0) lines.push(`… (${truncated} more nodes)`);
  return { docId, tree: lines.join("\n"), refs: refCount, nodes, truncated };
}

export const SNAPSHOT_SCRIPT_SOURCE = pageSnapshot.toString();
