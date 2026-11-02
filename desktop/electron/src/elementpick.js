"use strict";

// The page half of the element picker, kept as a function so it can be
// stringified into the page. The highlight and the hover card are drawn there
// because a native view sits above anything this window's own page draws.
//
// Nothing here may reach out of the function: the page runs it alone, with no
// module and no binding of this process to refer to.

function pagePicker(labels = {}) {
  // The window owns the words and sends them in: the page only draws them.
  const L = {
    color: "Color",
    background: "Background",
    font: "Font",
    frame: "Element inside an embedded frame — not supported",
    shadow: "Element inside a shadow root — not supported",
    ...labels,
  };
  const KEY = "__reasonixElementPicker";
  const CLIP = 4000;
  // Any words of its own already say what the element is, in any language;
  // only one that cannot speak — an icon, an image — is given the words
  // around it. A Chinese heading is a sentence in a dozen characters, so the
  // test is whether it has text at all, not how much.
  const NEARBY_OWN = 2;
  const NEARBY_LIMIT = 300;
  let settlePick = null;
  // The promise is the pick: it settles on the click, on Escape, or when the
  // page takes the picker away from under it.
  const answer = new Promise((done) => {
    settlePick = done;
  });
  const previous = window[KEY];
  if (previous) previous.stop();

  const TAG_ROLES = {
    A: "link",
    BUTTON: "button",
    IMG: "img",
    NAV: "navigation",
    MAIN: "main",
    FORM: "form",
    SELECT: "combobox",
    TEXTAREA: "textbox",
    H1: "heading",
    H2: "heading",
    H3: "heading",
    H4: "heading",
    H5: "heading",
    H6: "heading",
  };
  const INPUT_ROLES = {
    checkbox: "checkbox",
    radio: "radio",
    range: "slider",
    button: "button",
    submit: "button",
    reset: "button",
  };
  const KEPT = new Set(["id", "class", "href", "src", "alt", "title", "name", "type", "placeholder", "data-testid"]);
  // The roles an element can have while saying nothing to a reader by itself:
  // an icon button, an image, a field. Context is for these. A decorative
  // span has no role, and the page around it does not give it one.
  const SPEAKING = new Set(["button", "link", "textbox", "checkbox", "radio", "slider", "combobox", "img"]);

  const clip = (text, limit) => (text.length > limit ? text.slice(0, limit) : text);
  const textOf = (node) => (node.innerText || node.textContent || "").trim();

  // rgb()/rgba() as the page computed them, written the way a person reads a
  // colour; a translucent one is left as computed so its alpha is not lost.
  const hex = (css) => {
    const match = /^rgba?\(([^)]+)\)$/.exec(css || "");
    if (!match) return css || "";
    const parts = match[1].split(/[\s,/]+/).filter(Boolean).map(Number);
    if (parts.length > 3 && parts[3] < 1) return css;
    const [r, g, b] = parts;
    return ("#" + [r, g, b].map((one) => Math.round(one).toString(16).padStart(2, "0")).join("")).toUpperCase();
  };

  const roleOf = (el) => {
    const stated = el.getAttribute("role");
    if (stated) return stated;
    if (el.tagName === "INPUT") return INPUT_ROLES[el.type] || "textbox";
    return TAG_ROLES[el.tagName] || "";
  };

  const nameOf = (el) => {
    const ids = (el.getAttribute("aria-labelledby") || "").split(/\s+/).filter(Boolean);
    if (ids.length) {
      const text = ids
        .map((id) => {
          const node = document.getElementById(id);
          return node ? textOf(node) : "";
        })
        .join(" ")
        .trim();
      if (text) return clip(text, 500);
    }
    for (const attr of ["aria-label", "alt", "title", "placeholder"]) {
      const value = el.getAttribute(attr);
      if (value && value.trim()) return clip(value.trim(), 500);
    }
    return clip(textOf(el), 500);
  };

  // What the page declares as an identity outlives what it builds for styling:
  // a hash class is gone after the next deploy, a data-testid is not.
  const attrValue = (value) => '"' + value.replace(/["\\]/g, "\\$&") + '"';

  // A CSS path a person can paste, preferring the page's own identity over its
  // styling. Falls back to tag.class with :nth-of-type where a sibling would
  // answer the same, up to eight levels of parent.
  const selectorOf = (el) => {
    const testId = el.getAttribute("data-testid");
    if (testId) return { selector: `[data-testid=${attrValue(testId)}]`, basis: "testid" };
    const label = el.getAttribute("aria-label");
    if (label) return { selector: `${el.tagName.toLowerCase()}[aria-label=${attrValue(label)}]`, basis: "aria-label" };
    const own = el.getAttribute("id");
    if (own) return { selector: "#" + CSS.escape(own), basis: "id" };
    const parts = [];
    let node = el;
    for (let depth = 0; node && node.nodeType === 1 && depth < 8; depth++) {
      const id = node.getAttribute("id");
      if (id) {
        parts.unshift("#" + CSS.escape(id));
        break;
      }
      let part = node.tagName.toLowerCase();
      for (const name of [...node.classList].filter(Boolean).slice(0, 3)) part += "." + CSS.escape(name);
      const parent = node.parentElement;
      if (parent) {
        const same = [...parent.children].filter((child) => child.tagName === node.tagName);
        if (same.length > 1) part += `:nth-of-type(${same.indexOf(node) + 1})`;
      }
      parts.unshift(part);
      node = node.parentElement;
    }
    return { selector: parts.join(" > "), basis: "class" };
  };

  const xpathOf = (el) => {
    const parts = [];
    let node = el;
    for (let depth = 0; node && node.nodeType === 1 && depth < 12; depth++) {
      let index = 1;
      for (let sib = node.previousElementSibling; sib; sib = sib.previousElementSibling) {
        if (sib.tagName === node.tagName) index++;
      }
      parts.unshift(`${node.tagName.toLowerCase()}[${index}]`);
      node = node.parentElement;
    }
    return "/" + parts.join("/");
  };

  // What the element says. A password field never hands over what was typed in
  // it: the pick leaves with the fact that it was one, and nothing else.
  const textFor = (el) => {
    // An editable region holds what the person is writing right now.
    if (el.isContentEditable) return "[masked editable text]";
    if (el.tagName === "INPUT" && el.type === "password") return "[masked password input]";
    if (el.tagName === "INPUT" || el.tagName === "TEXTAREA") {
      return clip([el.getAttribute("aria-label"), el.getAttribute("placeholder"), el.getAttribute("name"), el.type].filter(Boolean).join(" "), CLIP);
    }
    return clip(textOf(el), CLIP);
  };

  // What surrounds an element, for the ones that cannot speak for themselves.
  // An element with text of its own asks for nothing: the nearest article
  // around a title is the whole page, which is not context but noise.
  // What surrounds an element, for the ones that cannot speak for themselves.
  // An element with text of its own asks for nothing, and neither does one
  // with no role at all: a decorative mark is not made meaningful by the page
  // around it, so the words there would be noise rather than context.
  const nearbyFor = (el) => {
    if (textOf(el).trim().length >= NEARBY_OWN) return "";
    if (!SPEAKING.has(roleOf(el))) return "";
    const near = el.closest("article, section, main, form, li, tr, dialog") || el.parentElement;
    return near ? clip(textOf(near), NEARBY_LIMIT) : "";
  };

  // The element's own markup with its scripts and the values a person typed
  // taken out, so what goes to the chat is structure, not their input.
  const excerptFor = (el) => {
    const clone = el.cloneNode(true);
    for (const drop of clone.querySelectorAll("script, style, noscript, template")) drop.remove();
    for (const field of clone.querySelectorAll("input")) field.removeAttribute("value");
    if (clone.tagName === "INPUT") clone.removeAttribute("value");
    for (const area of clone.querySelectorAll("textarea")) area.textContent = "";
    if (clone.isContentEditable) clone.textContent = "";
    return clip(clone.outerHTML, 6000);
  };

  const describe = (el) => {
    const rect = el.getBoundingClientRect();
    const computed = getComputedStyle(el);
    const attributes = {};
    for (const attr of el.attributes) {
      const name = attr.name.toLowerCase();
      if (name === "value") continue;
      if (!KEPT.has(name) && !name.startsWith("aria-")) continue;
      attributes[name] = clip(attr.value, 500);
    }
    const style = {
      color: hex(computed.color),
      display: computed.display,
      fontFamily: computed.fontFamily,
      fontSize: computed.fontSize,
      fontWeight: computed.fontWeight,
    };
    if (computed.backgroundColor && computed.backgroundColor !== "rgba(0, 0, 0, 0)" && computed.backgroundColor !== "transparent") {
      style.backgroundColor = hex(computed.backgroundColor);
    }
    const path = selectorOf(el);
    const info = {
      pageUrl: location.href,
      pageTitle: document.title,
      tagName: el.tagName.toLowerCase(),
      selector: path.selector,
      xpath: xpathOf(el),
      htmlExcerpt: excerptFor(el),
      attributes,
      rect: { x: rect.x, y: rect.y, width: rect.width, height: rect.height },
      style,
      capturedAt: Date.now(),
    };
    // Which of the four the selector came from, so a reader can tell a stable
    // identity from one that a deploy will change.
    info.selectorBasis = path.basis;
    // A field with no answer is left out rather than sent empty: "there is no
    // role" and "the role is an empty string" are different answers, and the
    // second is not one this page can give.
    const role = roleOf(el);
    if (role) info.role = role;
    const name = nameOf(el);
    if (name) info.accessibleName = name;
    const own = textFor(el);
    if (own) info.text = own;
    const nearby = nearbyFor(el);
    if (nearby) info.nearbyText = nearby;
    return info;
  };

  const box = document.createElement("div");
  box.dataset.reasonixPick = "box";
  box.setAttribute("aria-hidden", "true");
  box.style.cssText =
    "position:fixed;z-index:2147483646;pointer-events:none;border:2px solid #3b82f6;" +
    "border-radius:3px;background:rgba(59,130,246,.12);box-sizing:border-box";
  const card = document.createElement("div");
  card.dataset.reasonixPick = "card";
  card.setAttribute("aria-hidden", "true");
  card.style.cssText =
    "position:fixed;z-index:2147483647;pointer-events:none;background:#1f2328;color:#e6edf3;" +
    "border-radius:8px;padding:9px 12px;font:12px/1.6 ui-sans-serif,system-ui,sans-serif;" +
    "box-shadow:0 8px 24px rgba(0,0,0,.28);min-width:190px;max-width:340px";

  // One card line. The label is the quiet side, the value the loud one; both
  // are set as text, never as markup, because the page chose both.
  const line = (label, value, loud) => {
    const row = document.createElement("div");
    row.style.cssText = "display:flex;gap:12px;justify-content:space-between;align-items:baseline";
    const key = document.createElement("span");
    key.textContent = label;
    key.style.cssText = `flex:none;color:${loud ? "#e6edf3" : "#8b949e"};font-weight:${loud ? 600 : 400}`;
    const val = document.createElement("span");
    val.textContent = value;
    val.style.cssText =
      "color:#e6edf3;font-family:ui-monospace,SFMono-Regular,Menlo,monospace;overflow:hidden;" +
      "text-overflow:ellipsis;white-space:nowrap" + (loud ? ";font-weight:600" : "");
    row.append(key, val);
    card.append(row);
  };

  let target = null;
  // The chain of children walked up from, so the down keys can walk back.
  let descended = [];
  // Shift-click keeps a set: the person is naming a region, not one node.
  const chosen = [];
  const marks = [];
  let armed = true;
  let listeners = [];

  const clear = () => {
    for (const [type, handler, capture] of listeners) document.removeEventListener(type, handler, capture);
    listeners = [];
    for (const { cover } of frameCovers) cover.remove();
    frameCovers.length = 0;
    for (const at of marks) at.remove();
    marks.length = 0;
    box.remove();
    card.remove();
    if (window[KEY] && window[KEY].stop === stop) delete window[KEY];
  };

  const settle = (result) => {
    if (!armed) return;
    armed = false;
    clear();
    settlePick(result);
  };

  const stop = (reason = "dismissed") => settle({ cancelled: true, reason });

  // Where the card sits: under the element when the viewport has room for it,
  // above otherwise, and never past either edge.
  // A frame's own document takes the pointer, so the top document never hears a
  // move over it. A cover over each frame keeps the picker's events coming and
  // says why that area is not pickable instead of going quiet.
  const frameCovers = [];
  const positionCovers = () => {
    for (const { cover, frame } of frameCovers) {
      const rect = frame.getBoundingClientRect();
      cover.style.left = rect.left + "px";
      cover.style.top = rect.top + "px";
      cover.style.width = rect.width + "px";
      cover.style.height = rect.height + "px";
      cover.style.display = rect.width > 0 && rect.height > 0 ? "block" : "none";
    }
  };
  const coverFrames = () => {
    for (const { cover } of frameCovers) cover.remove();
    frameCovers.length = 0;
    for (const frame of document.querySelectorAll("iframe, frame")) {
      const cover = document.createElement("div");
      cover.dataset.reasonixFrame = "1";
      cover.style.cssText =
        "position:fixed;z-index:2147483646;pointer-events:auto;background:rgba(59,130,246,0.08);border:1px dashed rgba(59,130,246,0.55);box-sizing:border-box";
      cover.addEventListener("pointermove", () => show(frame), true);
      cover.addEventListener("click", (event) => {
        swallow(event);
        show(frame);
      }, true);
      document.documentElement.append(cover);
      frameCovers.push({ cover, frame });
    }
    positionCovers();
  };

  const place = () => {
    if (!target) return;
    const rect = target.getBoundingClientRect();
    const width = Math.max(0, rect.width);
    const height = Math.max(0, rect.height);
    box.style.left = rect.left + "px";
    box.style.top = rect.top + "px";
    box.style.width = width + "px";
    box.style.height = height + "px";
    card.style.visibility = "hidden";
    card.style.left = "0px";
    card.style.top = "0px";
    const size = card.getBoundingClientRect();
    const below = rect.bottom + 8;
    const top = below + size.height <= innerHeight ? below : Math.max(8, rect.top - size.height - 8);
    const left = Math.min(Math.max(8, rect.left), Math.max(8, innerWidth - size.width - 8));
    card.style.left = left + "px";
    card.style.top = top + "px";
    card.style.visibility = "visible";
  };

  // The chain from the document down to the element, short enough to read in
  // the card: a person walking up with the arrow keys needs to see where they
  // are, not the whole tree.
  const ancestorsOf = (el) => {
    const chain = [];
    for (let node = el; node && node.nodeType === 1; node = node.parentElement) {
      chain.unshift(node);
      if (chain.length > 8) break;
    }
    return chain;
  };
  const crumbLabel = (el) => el.tagName.toLowerCase() + (el.id ? "#" + el.id : "");

  // A frame or a shadow host cannot be read from here: the picker only ever
  // sees the host element, so it says so instead of handing over a useless
  // answer that looks like the real thing.
  const blockedOf = (el) => {
    if (!el) return "";
    if (el.tagName === "IFRAME" || el.tagName === "FRAME") return "iframe";
    if (el.shadowRoot) return "shadow";
    return "";
  };

  const show = (el) => {
    target = el;
    const rect = el.getBoundingClientRect();
    const computed = getComputedStyle(el);
    card.textContent = "";
    const blocked = blockedOf(el);
    if (blocked) {
      const note = document.createElement("div");
      note.textContent = blocked === "iframe" ? L.frame : L.shadow;
      note.style.cssText = "max-width:260px;color:#e6edf3;font-size:12px;line-height:1.5";
      card.append(note);
      box.style.display = "block";
      card.style.display = "block";
      place();
      return;
    }
    const crumb = document.createElement("div");
    crumb.style.cssText = "display:flex;gap:4px;overflow:hidden;white-space:nowrap;margin-bottom:4px;color:#8b949e;font-size:11px";
    const chain = ancestorsOf(el).slice(-5);
    chain.forEach((node, index) => {
      if (index) {
        const sep = document.createElement("span");
        sep.textContent = "›";
        sep.style.opacity = "0.6";
        crumb.append(sep);
      }
      const part = document.createElement("span");
      part.textContent = crumbLabel(node);
      if (node === el) {
        part.style.color = "#e6edf3";
        part.style.fontWeight = "600";
      }
      crumb.append(part);
    });
    card.append(crumb);
    line(el.tagName.toLowerCase(), `${Math.round(rect.width)}×${Math.round(rect.height)}`, true);
    line(L.color, hex(computed.color), false);
    if (computed.backgroundColor && computed.backgroundColor !== "rgba(0, 0, 0, 0)" && computed.backgroundColor !== "transparent") {
      line(L.background, hex(computed.backgroundColor), false);
    }
    line(L.font, `${computed.fontSize} ${computed.fontFamily}`, false);
    box.style.display = "block";
    card.style.display = "block";
    place();
  };

  document.documentElement.append(box, card);
  coverFrames();
  box.style.display = "none";
  card.style.display = "none";

  // Every gesture is taken from the page while the pick is armed: a click
  // chooses an element instead of pressing a button, and no page handler hears
  // it either. Escape is the way out that changes nothing.
  const swallow = (event) => {
    event.preventDefault();
    event.stopPropagation();
    event.stopImmediatePropagation();
  };
  let moveAt = null;
  let moveFrame = 0;
  const onMove = (event) => {
    // One hit test per frame: a page with a heavy layout is not asked to answer
    // a move it will not draw.
    moveAt = event;
    if (moveFrame) return;
    moveFrame = requestAnimationFrame(() => {
      moveFrame = 0;
      if (!armed) return;
      const at = moveAt;
      moveAt = null;
      if (!at) return;
      const el = document.elementFromPoint(at.clientX, at.clientY);
      if (!el || el === box || el === card) return;
      if (el.dataset && el.dataset.reasonixFrame) return;
      if (el === target) return;
      descended = [];
      show(el);
    });
  };
  // A picked element keeps a mark with its order, so a set stays visible while
  // the person keeps adding to it.
  const mark = (el) => {
    const rect = el.getBoundingClientRect();
    const at = document.createElement("div");
    at.dataset.reasonixPicked = "1";
    at.setAttribute("aria-hidden", "true");
    at.style.cssText = `position:fixed;z-index:2147483645;pointer-events:none;border:1px solid #22c55e;background:rgba(34,197,94,0.12);box-sizing:border-box;left:${rect.left}px;top:${rect.top}px;width:${rect.width}px;height:${rect.height}px`;
    const order = document.createElement("span");
    order.textContent = String(marks.length + 1);
    order.style.cssText = "position:absolute;top:-17px;left:-1px;background:#22c55e;color:#04170a;font:600 10px ui-sans-serif,system-ui,sans-serif;padding:1px 4px;border-radius:3px";
    at.append(order);
    document.documentElement.append(at);
    marks.push(at);
  };
  const onPick = (event) => {
    if (!armed) return;
    swallow(event);
    // The pointer may have climbed with the arrow keys: what is picked is the
    // level the person is looking at, not what happens to be under the cursor.
    try {
      // A frame or shadow host is not an answer: the click leaves the pick
      // armed while the card already says why.
      if (target) {
        if (blockedOf(target)) return;
        if (event.shiftKey) {
          chosen.push(target);
          mark(target);
          return;
        }
        return settle(chosen.length ? { cancelled: false, elements: [...chosen, target].map(describe) } : { cancelled: false, element: describe(target) });
      }
      const el = document.elementFromPoint(event.clientX, event.clientY);
      if (!el) return settle({ cancelled: true });
      if (blockedOf(el)) return;
      if (event.shiftKey) {
        chosen.push(el);
        mark(el);
        return;
      }
      settle(chosen.length ? { cancelled: false, elements: [...chosen, el].map(describe) } : { cancelled: false, element: describe(el) });
    } catch (error) {
      console.error("reasonix element pick failed", error);
      settle({ cancelled: true, reason: "gone" });
    }
  };
  const onKey = (event) => {
    if (event.key === "Escape") {
      swallow(event);
      stop("escape");
      return;
    }
    // Up walks to the parent and down back to the child it left, with the chain
    // drawn in the card so neither move is a guess.
    if (event.key === "ArrowUp" || event.key === "ArrowLeft") {
      swallow(event);
      const parent = target && target.parentElement;
      if (parent && parent !== document.documentElement) {
        descended.push(target);
        show(parent);
      }
      return;
    }
    if (event.key === "ArrowDown" || event.key === "ArrowRight") {
      swallow(event);
      const child = descended.pop();
      if (child && target && child.parentElement === target) show(child);
      return;
    }
    // Every other key belongs to the picker as well: Enter on a focused button
    // would run the page action a click is not allowed to run, and the pick is
    // not using the page.
    swallow(event);
  };
  const onScroll = () => {
    place();
    positionCovers();
  };

  for (const [type, handler, capture] of [
    ["pointerdown", swallow, true],
    ["pointerup", swallow, true],
    ["mousedown", swallow, true],
    ["mouseup", swallow, true],
    ["dblclick", swallow, true],
    ["contextmenu", swallow, true],
    ["click", onPick, true],
    ["pointermove", onMove, true],
    ["mousemove", onMove, true],
    ["keydown", onKey, true],
    ["scroll", onScroll, true],
    ["resize", onScroll, false],
  ]) {
    document.addEventListener(type, handler, capture);
    listeners.push([type, handler, capture]);
  }

  window[KEY] = { stop };
  return answer;
}

// ARM runs the picker in the page's own world; the promise it answers with is
// what executeJavaScript returns, so a pick takes as long as the person does.
const ARM = (labels) => `(${pagePicker.toString()})(${JSON.stringify(labels || {})})`;

// STOP disarms whatever picker is armed, settling that pick as cancelled.
const STOP = `(() => { const picker = window.__reasonixElementPicker; if (picker) picker.stop(); })()`;

module.exports = { ARM, STOP };
