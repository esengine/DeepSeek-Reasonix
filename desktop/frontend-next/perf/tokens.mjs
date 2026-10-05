// 变量的守卫：样式里每个不带兜底的 var(--x)，都得真有人给它赋过值。
//
// CSS 对这种错是沉默的：--x 没定义时整条声明被丢掉，颜色就退回继承来的那个，
// 看着像「设计如此」。仓库里曾同时躺着两个：连接提示那层模态写的是 var(--bg)，
// 于是它只有模糊没有压暗；用量面板那几处 color: var(--fg)，字色根本没落上。
//
// 名单都从源码读，不手抄：CSS 里的 `--x:` 是一处，JSX 里逐元素写进 style 的
// （Sky 的光柱那种）是另一处，两处都算数。手抄的名单会腐烂，源码不会。
import { readFileSync, readdirSync } from "node:fs";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const SRC = process.env.PERF_SRC ?? join(HERE, "..", "src");

const walk = (dir, ext) =>
  readdirSync(dir, { withFileTypes: true }).flatMap((e) =>
    e.isDirectory() ? walk(join(dir, e.name), ext) : e.name.endsWith(ext) ? [join(dir, e.name)] : [],
  );

const css = walk(join(SRC, "styles"), ".css").map((p) => [p, readFileSync(p, "utf8")]);
const code = [...walk(SRC, ".tsx"), ...walk(SRC, ".ts")].map((p) => readFileSync(p, "utf8")).join("\n");

// 注释要先去掉。一句说明「从来没有过 --bg」的注释里就带着 `--bg:`，而这份
// 守卫原样扫过去，于是**解释它不存在的那句话，成了它存在的证据** —— 七处
// var(--bg) 就这么一直绿着。
const bare = (text) => text.replace(/\/\*[\s\S]*?\*\//g, " ");

const declared = new Set();
for (const [, text] of css) for (const m of bare(text).matchAll(/(--[\w-]+)\s*:/g)) declared.add(m[1]);
// 逐元素写进 style 的：style={{ "--x": … }} 和 setProperty("--x", …)
for (const m of code.matchAll(/["'](--[\w-]+)["']\s*:/g)) declared.add(m[1]);
for (const m of code.matchAll(/setProperty\(\s*["'](--[\w-]+)["']/g)) declared.add(m[1]);

// 带兜底的 var(--x, …) 是明写的「可以没有」，只查不带兜底的那种。
const used = [];
for (const [path, text] of css)
  for (const m of bare(text).matchAll(/var\(\s*(--[\w-]+)\s*\)/g)) used.push([m[1], path]);

console.log(`定义过的变量 ${declared.size} 个   不带兜底地被读的 ${new Set(used.map(([v]) => v)).size} 个`);
// 一边扫成空说明路径挪了，不是「都对齐」。少了这一句，目录一改这份守卫就会
// 永远安静地绿着 —— 它比没有守卫更糟。
if (!declared.size || !used.length) {
  console.log("\n没扫到源码：至少一边是空的，先确认 PERF_SRC 的位置。");
  process.exit(1);
}

const seen = new Map();
for (const [v, path] of used) if (!declared.has(v) && !seen.has(v)) seen.set(v, path);
if (seen.size) {
  console.log(`\n没有任何人赋过值的 ${seen.size} 个（这些声明会被整条丢掉）：`);
  for (const [v, path] of seen) console.log(`  ${v}    ← ${path.slice(SRC.length + 1)}`);
  process.exit(1);
}
console.log("\n全部有主");
