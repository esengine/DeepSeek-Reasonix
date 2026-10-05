// 码的守卫：内核发出的每个拒绝码，前端都要有话说；前端也不该留着内核已经
// 不发的码。缺一条时界面会退回内核的英文兜底——能看懂，但不是中文，所以漏
// 了同样不会自己暴露。这就是替代品。
//
// 两侧都从源码读，而不是从一份手抄的清单读：清单会腐烂，源码不会。
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const SRC = process.env.PERF_SRC ?? join(HERE, "..", "src");
const KERNEL = process.env.PERF_KERNEL ?? join(HERE, "..", "..", "..", "internal", "serve");

function goFiles(dir) {
  return readdirSync(dir)
    .filter((n) => n.endsWith(".go") && !n.endsWith("_test.go"))
    .map((n) => join(dir, n))
    .filter((p) => statSync(p).isFile());
}

// refuse(w, status, "code", …) / busy(w, "code", …) / busyErr("code", …) /
// refusal(status, "code", …)
const EMIT = /\b(?:refuse\(\s*w\s*,[^,]+,|busy\(\s*w\s*,|busyErr\(|refusal\([^,]+,)\s*"([a-z][\w.]*)"/g;
const emitted = new Map();
for (const f of goFiles(KERNEL)) {
  const src = readFileSync(f, "utf8");
  for (const m of src.matchAll(EMIT)) emitted.set(m[1], f.split("/").pop());
}

const front = readFileSync(join(SRC, "i18n", "kernel.ts"), "utf8");
const known = new Set([...front.matchAll(/^\s{2}"([a-z][\w.]*)":/gm)].map((m) => m[1]));

const missing = [...emitted.keys()].filter((c) => !known.has(c)).sort();
const stale = [...known].filter((c) => !emitted.has(c)).sort();

console.log(`内核发出的码 ${emitted.size} 个   前端有话说的 ${known.size} 个`);
// 一边扫成空，说明路径挪了，不是「两侧对齐」。少了这一句，目录一改这份守卫
// 就永远安静地绿着 —— 它比没有守卫更糟，因为它替你担保了它其实没看的事。
if (!emitted.size || !known.size) {
  console.log("\n没扫到源码：至少一边是空的，先确认 PERF_KERNEL / src 的位置。");
  process.exit(1);
}
if (missing.length) {
  console.log(`\n前端没有对应说法的 ${missing.length} 个（会退回英文）：`);
  for (const c of missing) console.log(`  ${c}    ← ${emitted.get(c)}`);
}
if (stale.length) {
  console.log(`\n内核已经不发的 ${stale.length} 个：`);
  for (const c of stale) console.log(`  ${c}`);
}
if (!missing.length && !stale.length) console.log("\n两侧对齐");
process.exit(missing.length ? 1 : 0);
