import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const styles = readFileSync(fileURLToPath(new URL("../styles.css", import.meta.url)), "utf8");

assert.match(
  styles,
  /:root\[data-theme-style\] \.sidebar--workbench \.project-tree__topic--active \.project-tree__topic-label\s*\{[^}]*font-weight:\s*500;/s,
  "active and inactive workbench topics keep the same font weight",
);
assert.doesNotMatch(
  styles,
  /\.sidebar--workbench \.project-tree__topic--active \.project-tree__topic-label\s*\{[^}]*font-weight:\s*700;/s,
  "a later workbench rule cannot restore bold active text",
);
assert.match(
  styles,
  /\.sidebar--workbench \.project-tree__topic--active \.project-tree__topic-label\s*\{[^}]*font-weight:\s*500;/s,
  "the final workbench override also keeps active topics at the normal weight",
);
assert.match(
  styles,
  /:root\[data-theme-style\] \.sidebar--workbench \.project-tree__topic\s*\{[^}]*height:\s*34px;/s,
  "workbench topic rows keep a fixed height",
);

console.log("  PASS  project tree row stability contract");
