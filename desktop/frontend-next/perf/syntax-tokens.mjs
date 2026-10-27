// A pack's nine syntax colours must reach both places code is drawn — the file
// editor and a rendered code block — and a pack that names none must leave both
// exactly as they are. Only the browser resolves the variables, so only the
// browser can say so.
import { chromium } from "playwright";

const BASE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const NAMES = ["keyword", "string", "number", "function", "comment", "type", "operator", "punctuation", "variable"];
const TOKEN = { keyword: "synKeyword", string: "synString", number: "synNumber", function: "synFunction", comment: "synComment", type: "synType", operator: "synOperator", punctuation: "synPunctuation", variable: "synVariable" };
const HEX = ["#C01010", "#10A010", "#1010C0", "#C0A010", "#10A0A0", "#A010A0", "#808010", "#108060", "#6040C0"];
const SAMPLE = 'class Box {}\n// note\nfunction add(a) { return a + 1 }\nconst s = "x"; run(3);\n';
const HLJS = {
  keyword: "hljs-keyword", string: "hljs-string", number: "hljs-number", function: "hljs-title", comment: "hljs-comment",
  type: "hljs-type", operator: "hljs-operator", punctuation: "hljs-punctuation", variable: "hljs-variable",
};

const browser = await chromium.launch();
try {
  for (const scheme of ["light", "dark"]) {
    const page = await browser.newPage({ viewport: { width: 1200, height: 800 }, colorScheme: scheme });
    await page.goto(BASE, { waitUntil: "networkidle" });
    await page.waitForSelector(".app");
    const out = await page.evaluate(async ({ scheme, sample, hljs, names, token, hex }) => {
      const theme = window.__syntax;
      const { mount } = theme;
      const host = document.createElement("div");
      host.style.cssText = "position:fixed;left:0;top:0;width:700px;height:260px;z-index:2147483647";
      document.body.appendChild(host);
      mount(host, "probe.js", sample, scheme === "dark");
      const block = document.createElement("div");
      block.className = "md";
      block.style.cssText = "position:fixed;left:0;top:300px;width:700px;z-index:2147483647";
      block.innerHTML = `<pre class="term"><code>${names.map((n) => `<span class="${hljs[n]}" data-k="${n}">${n}</span>`).join(" ")}</code></pre>`;
      document.body.appendChild(block);
      for (let i = 0; i < 100 && !host.querySelector(".cm-line span"); i++) await new Promise((r) => setTimeout(r, 100));
      const rgb = (el) => getComputedStyle(el).color;
      const spanOf = (text, nth = 0) => [...host.querySelectorAll(".cm-line span")].filter((s) => s.textContent === text)[nth];
      const probe = {
        keyword: spanOf("class"), string: spanOf('"x"'), number: spanOf("3"), function: spanOf("run"), comment: spanOf("// note"),
        type: spanOf("Box"), operator: spanOf("+"), punctuation: spanOf("{"), variable: spanOf("a", 1) ?? spanOf("a"),
      };
      const read = () => ({
        editor: Object.fromEntries(names.map((n) => [n, probe[n] ? rgb(probe[n]) : null])),
        block: Object.fromEntries(names.map((n) => [n, rgb(block.querySelector(`[data-k="${n}"]`))])),
        inherited: rgb(block.querySelector("code")),
      });
      const tokens = (all) => Object.fromEntries(names.filter((n) => all || ["keyword", "string", "number", "function"].includes(n)).map((n) => [token[n], hex[names.indexOf(n)]]));
      const base = { bg: "#FFFFFF", bgSoft: "#F4F4F4", fg: "#1A1A1A", fgDim: "#5A5A5A", fgFaint: "#767676", accent: "#0066CC" };
      const pack = (all) => ({ id: "syn", name: "Syn", tokens: { light: { ...base, ...tokens(all) }, dark: { ...base, ...tokens(all) } } });
      const root = getComputedStyle(document.documentElement);
      const answer = {};
      theme.apply(null, scheme);
      answer.plain = read();
      answer.defaults = { faint: "", text: "", muted: "", fn: "" };
      const swatch = document.createElement("i");
      document.body.appendChild(swatch);
      for (const [k, v] of [["faint", "--faint"], ["text", "--text"], ["muted", "--muted"], ["fn", "--syn-fn"]]) { swatch.style.color = `var(${v})`; answer.defaults[k] = getComputedStyle(swatch).color; }
      theme.apply(pack(false), scheme);
      answer.four = read();
      answer.fourDefaults = {};
      swatch.style.color = "var(--syn-fn)"; answer.fourDefaults.fn = getComputedStyle(swatch).color;
      swatch.style.color = "var(--text)"; answer.fourDefaults.text = getComputedStyle(swatch).color;
      swatch.style.color = "var(--muted)"; answer.fourDefaults.muted = getComputedStyle(swatch).color;
      swatch.style.color = "var(--faint)"; answer.fourDefaults.faint = getComputedStyle(swatch).color;
      theme.apply(pack(true), scheme);
      answer.nine = read();
      theme.apply(null, scheme);
      void root;
      return answer;
    }, { scheme, sample: SAMPLE, hljs: HLJS, names: NAMES, token: TOKEN, hex: HEX });

    const asRgb = (h) => { const n = parseInt(h.slice(1), 16); return `rgb(${n >> 16}, ${(n >> 8) & 255}, ${n & 255})`; };
    for (const surface of ["editor", "block"]) {
      const seen = NAMES.map((n) => out.nine[surface][n]);
      check(`${scheme} ${surface}: nine syntax tokens give nine distinct colours`, new Set(seen).size === 9, seen.join(" "));
      for (const n of NAMES) check(`${scheme} ${surface}: ${n} takes the pack's colour`, out.nine[surface][n] === asRgb(HEX[NAMES.indexOf(n)]), `${out.nine[surface][n]} wanted ${asRgb(HEX[NAMES.indexOf(n)])}`);
    }
    // A pack that names only the four old tokens leaves each new one on what that surface drew before.
    const d = out.fourDefaults;
    check(`${scheme} editor: comment keeps the faint ink`, out.four.editor.comment === d.faint);
    check(`${scheme} editor: type keeps the function colour`, out.four.editor.type === d.fn);
    for (const n of ["operator", "punctuation", "variable"]) check(`${scheme} editor: ${n} keeps the text ink`, out.four.editor[n] === d.text, `${out.four.editor[n]} vs ${d.text}`);
    check(`${scheme} block: comment keeps the faint ink`, out.four.block.comment === d.faint);
    check(`${scheme} block: type keeps the function colour`, out.four.block.type === d.fn);
    for (const n of ["operator", "punctuation"]) check(`${scheme} block: ${n} keeps the muted ink`, out.four.block[n] === d.muted, `${out.four.block[n]} vs ${d.muted}`);
    check(`${scheme} block: a variable keeps the ink it inherits`, out.four.block.variable === out.four.inherited, `${out.four.block.variable} vs ${out.four.inherited}`);
    await page.close();
  }
} finally {
  await browser.close();
}
if (fails.length) {
  console.error(`\n${fails.length} failed: ${fails.join("; ")}`);
  process.exit(1);
}
console.log("\nall passed");
